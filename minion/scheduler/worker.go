package scheduler

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/network/plugin"
	"github.com/kelda/kelda/minion/vault"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
)

const labelKey = "kelda"
const labelValue = "scheduler"
const labelPair = labelKey + "=" + labelValue
const filesKey = "files"
const concurrencyLimit = 32

var once sync.Once

// evaluatedContainer represents a container as specified by the user, but
// with all references to secrets in Env and FilepathToContent evaluated
// to simple strings.
type evaluatedContainer struct {
	resolvedEnv, resolvedFilepathToContent map[string]string
	db.Container
}

// isWorkerReady waits until it can successfully connect to Vault. This way,
// any errors communicating with Vault within the module can be treated as real
// errors.
func isWorkerReady(conn db.Conn) bool {
	_, err := newVault(conn)
	if err == nil {
		return true
	}
	log.WithError(err).Debug("Failed to connect to Vault. This is " +
		"expected when the cluster has just booted, and the " +
		"leader has not yet started Vault.")
	return false
}

func runWorker(conn db.Conn, dk docker.Client, myPrivIP, myPubIP string) {
	if myPrivIP == "" || myPubIP == "" {
		return
	}
	myContainers := func(dbc db.Container) bool {
		return dbc.IP != "" && dbc.Minion == myPrivIP
	}

	vaultClient, err := newVault(conn)
	if err != nil {
		log.WithError(err).Error("Failed to connect to Vault")
		return
	}

	// In order for the flows installed by the plugin to work, the basic flows must
	// already be installed.  Thus, the first time we run we pre-populate the
	// OpenFlow table.
	once.Do(func() {
		updateOpenflow(conn, myPrivIP)
	})

	filter := map[string][]string{"label": {labelPair}}

	var toBoot, toKill []interface{}
	for i := 0; i < 2; i++ {
		dkcs, err := dk.List(filter)
		if err != nil {
			log.WithError(err).Warning("Failed to list docker containers.")
			return
		}

		// Get all the secret values referenced by the containers scheduled
		// for this minion. This must be done outside the database transaction
		// because it would be unreasonable to hold the database lock while
		// using the network to fetch the secret values. The secret values
		// are used to determine whether any containers have out of date
		// secret values, and thus need to be restarted.
		secretMap := resolveSecrets(
			vaultClient, conn.SelectFromContainer(myContainers))

		// Join the scheduled containers with the containers actually running
		// to figure out what containers to boot and stop.
		conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
			var readyToRun []evaluatedContainer
			for _, dbc := range view.SelectFromContainer(myContainers) {
				resolvedEnv, missingEnv := evaluateContainerValues(
					dbc.Env, secretMap, myPubIP)
				resolvedFiles, missingFiles := evaluateContainerValues(
					dbc.FilepathToContent, secretMap, myPubIP)

				missingSecrets := uniqueStrings(
					append(missingEnv, missingFiles...))
				if len(missingSecrets) != 0 {
					sort.Strings(missingSecrets)
					dbc.Status = fmt.Sprintf(
						"Waiting for secrets: %v", missingSecrets)
					view.Commit(dbc)
					continue
				}

				readyToRun = append(readyToRun, evaluatedContainer{
					resolvedEnv:               resolvedEnv,
					resolvedFilepathToContent: resolvedFiles,
					Container:                 dbc,
				})
			}

			var changed []db.Container
			changed, toBoot, toKill = syncWorker(readyToRun, dkcs)
			for _, dbc := range changed {
				view.Commit(dbc)
			}

			return nil
		})

		if len(toBoot) == 0 && len(toKill) == 0 {
			break
		}

		start := time.Now()
		doContainers(dk, toKill, dockerKill)
		doContainers(dk, toBoot, dockerRun)
		log.Infof("Scheduler spent %v starting/stopping containers",
			time.Since(start))
	}

	updateOpenflow(conn, myPrivIP)
}

func syncWorker(dbcs []evaluatedContainer, dkcs []docker.Container) (
	changed []db.Container, toBoot, toKill []interface{}) {

	var pairs []join.Pair
	pairs, toBoot, toKill = join.Join(dbcs, dkcs, syncJoinScore)

	for _, pair := range pairs {
		dbc := pair.L.(evaluatedContainer).Container
		dkc := pair.R.(docker.Container)

		dbc.DockerID = dkc.ID
		dbc.EndpointID = dkc.EID
		dbc.Status = dkc.Status
		dbc.Created = dkc.Created
		changed = append(changed, dbc)
	}

	return changed, toBoot, toKill
}

func doContainers(dk docker.Client, ifaces []interface{},
	do func(docker.Client, interface{})) {

	var wg sync.WaitGroup
	wg.Add(len(ifaces))
	defer wg.Wait()

	semaphore := make(chan struct{}, concurrencyLimit)
	for _, iface := range ifaces {
		semaphore <- struct{}{}
		go func(iface interface{}) {
			do(dk, iface)
			<-semaphore
			wg.Done()
		}(iface)
	}
}

func dockerRun(dk docker.Client, iface interface{}) {
	dbc := iface.(evaluatedContainer)
	log.WithField("container", dbc).Info("Start container")
	_, err := dk.Run(docker.RunOptions{
		Image:             dbc.Image,
		Args:              dbc.Command,
		Env:               dbc.resolvedEnv,
		FilepathToContent: dbc.resolvedFilepathToContent,
		Labels: map[string]string{
			labelKey: labelValue,
			filesKey: filesHash(dbc.resolvedFilepathToContent),
		},
		IP:          dbc.IP,
		NetworkMode: plugin.NetworkName,
		DNS:         []string{ipdef.GatewayIP.String()},
		DNSSearch:   []string{"q"},
	})
	if err != nil {
		log.WithFields(log.Fields{
			"error":     err,
			"container": dbc,
		}).WithError(err).Warning("Failed to run container")
	}
}

func dockerKill(dk docker.Client, iface interface{}) {
	dkc := iface.(docker.Container)
	log.WithField("container", dkc.ID).Info("Remove container")
	if err := dk.RemoveID(dkc.ID); err != nil {
		log.WithFields(log.Fields{
			"error": err,
			"id":    dkc.ID,
		}).Warning("Failed to remove container.")
	}
}

func syncJoinScore(left, right interface{}) int {
	dbc := left.(evaluatedContainer)
	dkc := right.(docker.Container)

	expFilesHash := filesHash(dbc.resolvedFilepathToContent)
	if dbc.IP != dkc.IP || expFilesHash != dkc.Labels[filesKey] {
		return -1
	}

	compareIDs := dbc.ImageID != ""
	namesMatch := dkc.Image == dbc.Image
	idsMatch := dkc.ImageID == dbc.ImageID
	if (compareIDs && !idsMatch) || (!compareIDs && !namesMatch) {
		return -1
	}

	for key, value := range dbc.resolvedEnv {
		if dkc.Env[key] != value {
			return -1
		}
	}

	// Depending on the container, the command in the database could be
	// either the command plus it's arguments, or just it's arguments.  To
	// handle that case, we check both.
	cmd1 := dkc.Args
	cmd2 := append([]string{dkc.Path}, dkc.Args...)
	if len(dbc.Command) != 0 &&
		!str.SliceEq(dbc.Command, cmd1) &&
		!str.SliceEq(dbc.Command, cmd2) {
		return -1
	}

	return 0
}

func filesHash(filepathToContent map[string]string) string {
	toHash := str.MapAsString(filepathToContent)
	return fmt.Sprintf("%x", sha1.Sum([]byte(toHash)))
}

func updateOpenflow(conn db.Conn, myIP string) {
	var dbcs []db.Container
	var conns []db.Connection

	txn := func(view db.Database) error {
		conns = view.SelectFromConnection(nil)
		dbcs = view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.EndpointID != "" && dbc.IP != "" && dbc.Minion == myIP
		})
		return nil
	}
	conn.Txn(db.ConnectionTable, db.ContainerTable).Run(txn)

	ofcs := openflowContainers(dbcs, conns)
	if err := replaceFlows(ofcs); err != nil {
		log.WithError(err).Warning("Failed to update OpenFlow")
	}
}

func openflowContainers(dbcs []db.Container,
	conns []db.Connection) []openflow.Container {

	fromPubPorts := map[string][]int{}
	toPubPorts := map[string][]int{}
	for _, conn := range conns {
		for _, from := range conn.From {
			for _, to := range conn.To {
				if from != blueprint.PublicInternetLabel &&
					to != blueprint.PublicInternetLabel {
					continue
				}

				if conn.MinPort != conn.MaxPort {
					c.Inc("Unsupported Public Port Range")
					log.WithField("connection", conn).Debug(
						"Unsupported Public Port Range")
					continue
				}

				if from == blueprint.PublicInternetLabel {
					fromPubPorts[to] = append(fromPubPorts[to],
						conn.MinPort)
				}

				if to == blueprint.PublicInternetLabel {
					toPubPorts[from] = append(toPubPorts[from],
						conn.MinPort)
				}
			}
		}
	}

	var ofcs []openflow.Container
	for _, dbc := range dbcs {
		_, peerKelda := ipdef.PatchPorts(dbc.EndpointID)

		ofc := openflow.Container{
			Veth:  ipdef.IFName(dbc.EndpointID),
			Patch: peerKelda,
			Mac:   ipdef.IPStrToMac(dbc.IP),
			IP:    dbc.IP,

			ToPub:   map[int]struct{}{},
			FromPub: map[int]struct{}{},
		}

		for _, p := range toPubPorts[dbc.Hostname] {
			ofc.ToPub[p] = struct{}{}
		}

		for _, p := range fromPubPorts[dbc.Hostname] {
			ofc.FromPub[p] = struct{}{}
		}

		ofcs = append(ofcs, ofc)
	}
	return ofcs
}

func uniqueStrings(lst []string) (unique []string) {
	set := map[string]struct{}{}
	for _, item := range lst {
		set[item] = struct{}{}
	}

	for item := range set {
		unique = append(unique, item)
	}
	return unique
}

// newVault gets a Vault client connected to the leader of the cluster.
var newVault = func(conn db.Conn) (vault.SecretStore, error) {
	etcds := conn.SelectFromEtcd(nil)
	if len(etcds) == 0 || etcds[0].LeaderIP == "" {
		return nil, errors.New("no cluster leader")
	}

	return vault.New(etcds[0].LeaderIP)
}

var replaceFlows = openflow.ReplaceFlows
