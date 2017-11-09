package foreman

import (
	"reflect"
	"sync"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/pb"

	log "github.com/sirupsen/logrus"
)

// Credentials that the foreman should use to connect to its minions.
var credentials connection.Credentials

type client interface {
	setMinion(pb.MinionConfig) error
	getMinion() (pb.MinionConfig, error)
	Close()
}

type clientImpl struct {
	pb.MinionClient
	cc *grpc.ClientConn
}

// The minion information that is shared between threads.
type minionStatus struct {
	connected bool
	role      db.Role
}

// A map from cloud ID to the corresponding `minionStatus`. This map is shared
// across threads, so all accesses should be locked with the `statusLock`.
var minionStatuses = map[string]minionStatus{}
var statusLock = &sync.Mutex{}

var c = counter.New("Foreman")

// Run checks for updates to the machine table and starts and stops minion threads
// in response.
func Run(conn db.Conn, creds connection.Credentials) {
	credentials = creds
	// A map from cloud ID to the stop channel for the corresponding minion thread.
	minionChans := make(map[string]chan struct{})

	for range conn.Trigger(db.MachineTable).C {
		machines := conn.SelectFromMachine(func(m db.Machine) bool {
			return m.PublicIP != "" && m.PrivateIP != "" &&
				m.CloudID != "" && m.Status != db.Stopping
		})
		updateMinions(conn, machines, minionChans)
	}
}

func updateMinions(conn db.Conn, machines []db.Machine,
	minionChans map[string]chan struct{}) {

	seen := make(map[string]struct{})
	for _, m := range machines {
		_, ok := minionChans[m.CloudID]

		if !ok {
			stop := make(chan struct{})
			minionChans[m.CloudID] = stop
			go newMinion(conn, m.CloudID, stop)
		}

		seen[m.CloudID] = struct{}{}
	}

	for id := range minionChans {
		if _, ok := seen[id]; !ok {
			close(minionChans[id])
			delete(minionChans, id)
		}
	}
}

var newMinion = newMinionImpl

func newMinionImpl(conn db.Conn, cloudID string, stop chan struct{}) {
	// Threads that aren't currently connected to their minion should run more often.
	frequentTick := time.NewTicker(5 * time.Second)
	tableTrigger := conn.TriggerTick(60, db.BlueprintTable, db.MachineTable)
	defer frequentTick.Stop()
	defer tableTrigger.Stop()

	var connected bool
	for {
		select {
		case <-stop:
		case <-frequentTick.C:
			if connected {
				continue
			}
		case <-tableTrigger.C:
		}

		// In a race between a closed stop and a trigger, choose stop.
		select {
		case <-stop:
			return
		default:
		}

		currConfig, connected := runOnce(conn, cloudID)
		setMinionStatus(cloudID, currConfig, connected)
	}
}

func runOnce(conn db.Conn, cloudID string) (currConfig pb.MinionConfig, connected bool) {
	currConfig = pb.MinionConfig{}
	connected = false

	var blueprint string
	var machines []db.Machine
	conn.Txn(db.BlueprintTable, db.MachineTable).Run(func(view db.Database) error {
		bp, _ := view.GetBlueprint()
		blueprint = bp.Blueprint.String()

		machines = view.SelectFromMachine(func(m db.Machine) bool {
			return m.CloudID != "" && m.PublicIP != "" &&
				m.PrivateIP != "" && m.Status != db.Stopping
		})
		return nil
	})

	var minionMachine db.Machine
	var found bool
	for _, m := range machines {
		if m.CloudID == cloudID {
			minionMachine = m
			found = true
		}
	}
	if !found {
		log.Debugf("Failed to get machine with ID %s", cloudID)
		return
	}

	cli, err := newClient(minionMachine.PublicIP)
	if err != nil {
		log.WithError(err).Debugf("Failed to connect to minion %s", cloudID)
		return
	}
	defer cli.Close()

	currConfig, err = cli.getMinion()
	if err != nil {
		log.WithError(err).Debug("Failed to get minion config")
	}

	connected = err == nil
	if !connected {
		return
	}

	newConfig := makeConfig(machines, minionMachine, blueprint)
	if !reflect.DeepEqual(currConfig, newConfig) {
		err = cli.setMinion(newConfig)
		if err != nil {
			log.WithError(err).Debug("Failed to set minion config.")
		}
	}
	return
}

func makeConfig(machines []db.Machine, minionMachine db.Machine,
	blueprint string) pb.MinionConfig {

	minionIPToPublicKey := map[string]string{}
	var etcdIPs []string
	for _, m := range machines {
		if m.Role == db.Master && m.PrivateIP != "" {
			etcdIPs = append(etcdIPs, m.PrivateIP)
		}

		if m.PrivateIP != "" && m.PublicKey != "" {
			minionIPToPublicKey[m.PrivateIP] = m.PublicKey
		}
	}

	return pb.MinionConfig{
		FloatingIP:          minionMachine.FloatingIP,
		PrivateIP:           minionMachine.PrivateIP,
		PublicIP:            minionMachine.PublicIP,
		Blueprint:           blueprint,
		Provider:            string(minionMachine.Provider),
		Size:                minionMachine.Size,
		Region:              minionMachine.Region,
		EtcdMembers:         etcdIPs,
		AuthorizedKeys:      minionMachine.SSHKeys,
		MinionIPToPublicKey: minionIPToPublicKey,
	}
}

func setMinionStatus(cloudID string, config pb.MinionConfig, isConnected bool) {
	statusLock.Lock()
	defer statusLock.Unlock()
	minionStatuses[cloudID] = minionStatus{
		connected: isConnected,
		role:      db.PBToRole(config.Role),
	}
}

// GetMachineRole uses the minionStatuses map to find the minion associated with
// the given cloud ID, according to the minion thread's last update cycle.
func GetMachineRole(cloudID string) db.Role {
	statusLock.Lock()
	defer statusLock.Unlock()
	if min, ok := minionStatuses[cloudID]; ok {
		return min.role
	}
	return db.None
}

// IsConnected returns whether the foreman is connected to the minion running
// on the machine with the given cloud ID.
func IsConnected(cloudID string) bool {
	statusLock.Lock()
	defer statusLock.Unlock()
	minion, ok := minionStatuses[cloudID]
	return ok && minion.connected
}

func newClientImpl(ip string) (client, error) {
	c.Inc("New Minion Client")
	cc, err := connection.Client("tcp", ip+":9999", credentials.ClientOpts())
	if err != nil {
		c.Inc("New Minion Client Error")
		return nil, err
	}

	return clientImpl{pb.NewMinionClient(cc), cc}, nil
}

// Storing in a variable allows us to mock it out for unit tests
var newClient = newClientImpl

func (cl clientImpl) getMinion() (pb.MinionConfig, error) {
	c.Inc("Get Minion")
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	cfg, err := cl.GetMinionConfig(ctx, &pb.Request{})
	if err != nil {
		c.Inc("Get Minion Error")
		return pb.MinionConfig{}, err
	}

	return *cfg, nil
}

func (cl clientImpl) setMinion(cfg pb.MinionConfig) error {
	c.Inc("Set Minion")
	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	_, err := cl.SetMinionConfig(ctx, &cfg)
	if err != nil {
		c.Inc("Set Minion Error")
	}
	return err
}

func (cl clientImpl) Close() {
	c.Inc("Close Client")
	cl.cc.Close()
}
