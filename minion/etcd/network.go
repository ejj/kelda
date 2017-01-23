package etcd

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"path"
	"sort"
	"strconv"
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
)

const (
	containerStore = "/containers"
	nodeStore      = "/nodes"
	minionIPStore  = "ips"
)

// wakeChan collapses the various channels these functions wait on into a single
// channel. Multiple redundant pings will be coalesced into a single message.
func wakeChan(conn db.Conn, store Store) chan struct{} {
	minionWatch := store.Watch("/", 1*time.Second)
	trigg := conn.TriggerTick(30, db.MinionTable, db.ContainerTable, db.LabelTable,
		db.EtcdTable).C

	c := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-minionWatch:
			case <-trigg:
			}

			select {
			case c <- struct{}{}:
			default: // There's a notification in queue, no need for another.
			}
		}
	}()

	return c
}

func runNetwork(conn db.Conn, store Store) {
	for range wakeChan(conn, store) {
		// If the etcd read failed, we only want to update the db if it
		// failed because a key was missing (has not been created yet).
		// In all other cases, we skip this iteration.
		etcdDBCs, err := readEtcd(store)
		if err != nil {
			etcdErr, ok := err.(client.Error)
			if !ok || etcdErr.Code != client.ErrorCodeKeyNotFound {
				log.WithError(err).Error("Etcd transaction failed.")
				continue
			}
			log.WithError(err).Debug()
		}

		leader := false
		var containers []db.Container
		conn.Txn(db.ContainerTable, db.EtcdTable, db.LabelTable,
			db.MinionTable).Run(func(view db.Database) error {

			leader = view.EtcdLeader()
			containers = view.SelectFromContainer(func(c db.Container) bool {
				return c.Minion != ""
			})

			minion, err := view.MinionSelf()
			if err == nil && minion.Role == db.Worker {
				updateWorker(view, minion, store, etcdDBCs)
			}

			ipMap, err := loadMinionIPs(store)
			if err != nil {
				log.WithError(err).Error("Etcd read minion IPs failed")
				return nil
			}

			// It would likely be more efficient to perform the etcd write
			// outside of the DB transact. But, if we perform the writes
			// after the transact, there is no way to ensure that the writes
			// were successful before updating the DB with the information
			// produced by the updateEtcd* functions (not considering the
			// etcd writes they perform).
			if leader {
				etcdDBCs, err = updateEtcdContainer(store, etcdDBCs,
					containers)
				if err != nil {
					log.WithError(err).Error("Etcd update failed.")
					return nil
				}

				updateLeaderDBC(view, containers, etcdDBCs, ipMap)
			}

			updateDBLabels(view, etcdDBCs, ipMap)
			return nil
		})
	}
}

func readEtcd(store Store) ([]db.Container, error) {
	containers, err := store.Get(containerStore)

	// Failed store reads will just be skipped by Unmarshal, which is fine
	// since an error is returned
	etcdContainerSlice := []db.Container{}
	json.Unmarshal([]byte(containers), &etcdContainerSlice)
	return etcdContainerSlice, err
}

func loadMinionIPs(store Store) (map[string]string, error) {
	ipMap := map[string]string{}
	allMinions, err := store.GetTree(nodeStore)
	if err != nil {
		return ipMap, err
	}

	for _, t := range allMinions.Children {
		minionData, ok := t.Children[selfNode]
		if !ok {
			log.Debugf("Minion %s has no self node in Etcd", t.Key)
			continue
		}

		var minion db.Minion
		err := json.Unmarshal([]byte(minionData.Value), &minion)
		if err != nil {
			log.Errorf("Failed to unmarshal minion %s self", t.Key)
			return ipMap, err
		}

		if minion.Role != db.Worker {
			continue
		}

		minionIPData, ok := t.Children[minionIPStore]
		if !ok {
			continue
		}

		minionIPMap := map[string]string{}
		err = json.Unmarshal([]byte(minionIPData.Value), &minionIPMap)
		if err != nil {
			log.Errorf("Failed to unmarshal minion %s IP data", t.Key)
			return ipMap, err
		}

		for stitchID, ipAddr := range minionIPMap {
			ipMap[stitchID] = ipAddr
		}
	}

	return ipMap, nil
}

func updateEtcdContainer(s Store, etcdDBCs []db.Container,
	dbcs []db.Container) ([]db.Container, error) {

	// XXX: On masters, the database has the container IPs that we learned from the
	// workers.  It likely won't hurt, but it's best not to write these IP addresses
	// to Etcd.  Note that this hack is a temporary measure, in future patches the
	// master will allocate database IPs in which case writing them will be
	// appropriate.
	//
	// Also note that we clear the dbc.ID so that it's not returned by this function.
	// Also a temporary hack fixed in future patches.
	var containers []db.Container
	for _, dbc := range dbcs {
		dbc.ID = 0
		dbc.IP = ""
		containers = append(containers, dbc)
	}
	sort.Sort(db.ContainerSlice(containers))

	newContainers, _ := jsonMarshal(containers)
	oldContainers, _ := jsonMarshal(etcdDBCs)
	if string(newContainers) == string(oldContainers) {
		return etcdDBCs, nil
	}

	if err := s.Set(containerStore, string(newContainers), 0); err != nil {
		return etcdDBCs, err
	}

	etcdDBCs = containers
	return etcdDBCs, nil

}

func updateLeaderDBC(view db.Database, dbcs []db.Container,
	etcdDBCs []db.Container, ipMap map[string]string) {

	for _, dbc := range dbcs {
		ipVal := ipMap[strconv.Itoa(dbc.StitchID)]
		if dbc.IP != ipVal {
			dbc.IP = ipVal
			view.Commit(dbc)
		}
	}
}

func updateWorker(view db.Database, self db.Minion, store Store,
	etcdDBCs []db.Container) {

	var containers []db.Container
	for _, etcdc := range etcdDBCs {
		if etcdc.Minion == self.PrivateIP {
			containers = append(containers, etcdc)
		}
	}

	pairs, dbcs, etcdcs := join.Join(view.SelectFromContainer(nil), containers,
		func(left, right interface{}) int {
			dbc := left.(db.Container)
			l := db.Container{
				StitchID: dbc.StitchID,
				Minion:   dbc.Minion,
				Image:    dbc.Image,
				Command:  dbc.Command,
				Env:      dbc.Env,
				Labels:   dbc.Labels,
			}
			return containerJoinScore(l, right.(db.Container))
		})

	for _, i := range dbcs {
		dbc := i.(db.Container)
		view.Remove(dbc)
	}

	for _, etcdc := range etcdcs {
		pairs = append(pairs, join.Pair{
			L: view.InsertContainer(),
			R: etcdc,
		})
	}

	for _, pair := range pairs {
		dbc := pair.L.(db.Container)
		etcdc := pair.R.(db.Container)

		dbc.StitchID = etcdc.StitchID
		dbc.Minion = etcdc.Minion
		dbc.Image = etcdc.Image
		dbc.Command = etcdc.Command
		dbc.Env = etcdc.Env
		dbc.Labels = etcdc.Labels

		view.Commit(dbc)
	}

	updateContainerIP(view.SelectFromContainer(nil), self.PrivateIP, store)
}

func updateContainerIP(containers []db.Container, privateIP string, store Store) {

	oldIPMap := map[string]string{}
	selfStore := path.Join(nodeStore, privateIP)
	etcdIPs, err := store.Get(path.Join(selfStore, minionIPStore))
	if err != nil {
		etcdErr, ok := err.(client.Error)
		if !ok || etcdErr.Code != client.ErrorCodeKeyNotFound {
			log.WithError(err).Error("Failed to load current IPs from Etcd")
			return
		}
	}
	json.Unmarshal([]byte(etcdIPs), &oldIPMap)

	newIPMap := map[string]string{}
	for _, c := range containers {
		newIPMap[strconv.Itoa(c.StitchID)] = c.IP
	}

	if util.StrStrMapEqual(oldIPMap, newIPMap) {
		return
	}

	jsonData, err := jsonMarshal(newIPMap)
	if err != nil {
		log.WithError(err).Error("Failed to marshal minion container IP map")
		return
	}

	err = store.Set(path.Join(selfStore, minionIPStore), string(jsonData), 0)
	if err != nil {
		log.WithError(err).Error("Failed to update minion container IP map")
	}
}

func updateDBLabels(view db.Database, etcdDBCs []db.Container, ipMap map[string]string) {
	// Gather all of the label keys and IPs for single host labels, and IPs of
	// the containers in a given label.
	containerIPs := map[string][]string{}
	labelKeys := map[string]struct{}{}
	for _, c := range etcdDBCs {
		for _, l := range c.Labels {
			labelKeys[l] = struct{}{}
			cIP := ipMap[strconv.Itoa(c.StitchID)]

			// The ordering of IPs between function calls will be consistent
			// because the containers are sorted by their StitchIDs when
			// inserted into etcd.
			containerIPs[l] = append(containerIPs[l], cIP)
		}
	}

	labelKeyFunc := func(val interface{}) interface{} {
		return val.(db.Label).Label
	}

	labelKeySlice := join.StringSlice{}
	for l := range labelKeys {
		labelKeySlice = append(labelKeySlice, l)
	}

	pairs, dbls, dirKeys := join.HashJoin(db.LabelSlice(view.SelectFromLabel(nil)),
		labelKeySlice, labelKeyFunc, nil)

	for _, dbl := range dbls {
		view.Remove(dbl.(db.Label))
	}

	for _, key := range dirKeys {
		pairs = append(pairs, join.Pair{L: view.InsertLabel(), R: key})
	}

	for _, pair := range pairs {
		dbl := pair.L.(db.Label)
		dbl.Label = pair.R.(string)
		dbl.ContainerIPs = containerIPs[dbl.Label]

		// XXX: In effect, we're implementing a dumb load balancer where all
		// traffic goes to the first container.  Something more sophisticated is
		// coming (hopefully).
		dbl.IP = ""
		if len(dbl.ContainerIPs) > 0 {
			dbl.IP = dbl.ContainerIPs[0]
		}
		view.Commit(dbl)
	}
}

func allocateIP(ipSet map[string]struct{}, subnet net.IPNet) (string, error) {
	prefix := binary.BigEndian.Uint32(subnet.IP.To4())
	mask := binary.BigEndian.Uint32(subnet.Mask)

	randStart := rand32() & ^mask
	for offset := uint32(0); offset <= ^mask; offset++ {

		randIP32 := ((randStart + offset) & ^mask) | (prefix & mask)

		randIP := net.IP(make([]byte, 4))
		binary.BigEndian.PutUint32(randIP, randIP32)
		randIPStr := randIP.String()

		if _, ok := ipSet[randIPStr]; !ok {
			ipSet[randIPStr] = struct{}{}
			return randIPStr, nil
		}
	}
	return "", errors.New("IP pool exhausted")
}

func containerJoinScore(left, right db.Container) int {
	if left.Minion != right.Minion ||
		left.Image != right.Image ||
		!util.StrSliceEqual(left.Command, right.Command) ||
		!util.StrStrMapEqual(left.Env, right.Env) {
		return -1
	}

	score := util.EditDistance(left.Labels, right.Labels)
	if left.StitchID != right.StitchID {
		score++
	}
	return score
}
