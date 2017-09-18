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

var minions map[string]*minion

// Credentials that the foreman should use to connect to its minions.
var Credentials connection.Credentials

// ConnectionTrigger sends messages when a change to the connection status of a
// minion occurs.
// The sends are non-blocking, so if there is already a notification in the
// queue, another one isn't sent. This is reasonable because the purpose of
// this trigger is just to notify the cluster that a change to the connection
// status of some machines have changed -- the cluster checks all machines when
// updating the status.
var ConnectionTrigger = make(chan struct{}, 1)

type client interface {
	setMinion(pb.MinionConfig) error
	getMinion() (pb.MinionConfig, error)
	Close()
}

type clientImpl struct {
	pb.MinionClient
	cc *grpc.ClientConn
}

type minion struct {
	client    client
	connected bool

	machine db.Machine
	config  pb.MinionConfig

	mark bool /* Mark and sweep garbage collection. */
}

var c = counter.New("Foreman")

// Init the first time the foreman operates on a new namespace.  It queries the currently
// running VMs for their previously assigned roles, and writes them to the database.
func Init(conn db.Conn) {
	c.Inc("Initialize")

	for _, m := range minions {
		m.client.Close()
	}
	minions = map[string]*minion{}

	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.PublicIP != "" && m.PrivateIP != "" &&
				m.CloudID != "" && m.Status != db.Stopping
		})

		updateMinionMap(machines)
		forEachMinion(updateConfig)
		return nil
	})
}

// RunOnce should be called regularly to allow the foreman to update minion cfg.
func RunOnce(conn db.Conn) {
	c.Inc("Run")

	var blueprint string
	var machines []db.Machine
	conn.Txn(db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {

		machines = view.SelectFromMachine(func(m db.Machine) bool {
			return m.PublicIP != "" && m.PrivateIP != ""
		})

		bp, _ := view.GetBlueprint()
		blueprint = bp.Blueprint.String()

		return nil
	})

	updateMinionMap(machines)
	forEachMinion(updateConfig)

	var etcdIPs []string
	for _, m := range minions {
		if m.config.Role == pb.MinionConfig_MASTER && m.machine.PrivateIP != "" {
			etcdIPs = append(etcdIPs, m.machine.PrivateIP)
		}
	}

	// Assign all of the minions their new configs
	forEachMinion(func(m *minion) {
		if !m.connected {
			return
		}

		newConfig := pb.MinionConfig{
			FloatingIP:     m.machine.FloatingIP,
			PrivateIP:      m.machine.PrivateIP,
			Blueprint:      blueprint,
			Provider:       string(m.machine.Provider),
			Size:           m.machine.Size,
			Region:         m.machine.Region,
			EtcdMembers:    etcdIPs,
			AuthorizedKeys: m.machine.SSHKeys,
		}

		if reflect.DeepEqual(newConfig, m.config) {
			return
		}

		if err := m.client.setMinion(newConfig); err != nil {
			log.WithError(err).Error("Failed to set minion config.")
			return
		}
	})
}

// GetMachineRole uses the minion map to find the associated minion with
// the IP, according to the foreman's last update cycle.
func GetMachineRole(pubIP string) db.Role {
	if min, ok := minions[pubIP]; ok {
		return db.PBToRole(min.config.Role)
	}
	return db.None
}

// IsConnected returns whether the foreman is connected to the minion at pubIP.
func IsConnected(pubIP string) bool {
	min, ok := minions[pubIP]
	return ok && min.connected
}

func updateMinionMap(machines []db.Machine) {
	for _, m := range machines {
		min, ok := minions[m.PublicIP]
		if !ok {
			client, err := newClient(m.PublicIP)
			if err != nil {
				continue
			}
			min = &minion{client: client}
			minions[m.PublicIP] = min
		}

		min.machine = m
		min.mark = true
	}

	for k, minion := range minions {
		if minion.mark {
			minion.mark = false
		} else {
			minion.client.Close()
			delete(minions, k)
		}
	}
}

func forEachMinion(do func(minion *minion)) {
	var wg sync.WaitGroup
	wg.Add(len(minions))
	for _, m := range minions {
		go func(m *minion) {
			do(m)
			wg.Done()
		}(m)
	}
	wg.Wait()
}

func updateConfig(m *minion) {
	var err error
	m.config, err = m.client.getMinion()
	if err != nil {
		if m.connected {
			log.WithError(err).Error("Failed to get minion config")
		} else {
			log.WithError(err).Debug("Failed to get minion config")
		}
	}

	connected := err == nil
	if connected == m.connected {
		return
	}

	m.connected = connected
	notifyConnectionChange()
	if m.connected {
		c.Inc("Minion Connected")
		log.WithField("machine", m.machine).Debug("New connection")
	} else {
		c.Inc("Minion Disconnected")
	}
}

func notifyConnectionChange() {
	select {
	case ConnectionTrigger <- struct{}{}:
	default:
	}
}

func newClientImpl(ip string) (client, error) {
	c.Inc("New Minion Client")
	cc, err := connection.Client("tcp", ip+":9999", Credentials.ClientOpts())
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
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	cfg, err := cl.GetMinionConfig(ctx, &pb.Request{})
	if err != nil {
		c.Inc("Get Minion Error")
		return pb.MinionConfig{}, err
	}

	return *cfg, nil
}

func (cl clientImpl) setMinion(cfg pb.MinionConfig) error {
	c.Inc("Set Minion")
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
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
