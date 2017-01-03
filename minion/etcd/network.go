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
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
)

const (
	minionDir      = "/minion"
	labelToIPStore = minionDir + "/labelIP"
	containerStore = minionDir + "/container"
	nodeStore      = minionDir + "/nodes"
	minionIPStore  = "ips"
)

// Keeping all the store data types in a struct makes it much less verbose to pass them
// around while operating on them
type storeData struct {
	containers []storeContainer
	multiHost  map[string]string
}

type storeContainer struct {
	StitchID int

	Minion  string
	Image   string
	Command []string
	Env     map[string]string

	Labels []string
}

type storeContainerSlice []storeContainer

// wakeChan collapses the various channels these functions wait on into a single
// channel. Multiple redundant pings will be coalesced into a single message.
func wakeChan(conn db.Conn, store Store) chan struct{} {
	minionWatch := store.Watch(minionDir, 1*time.Second)
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
		etcdData, err := readEtcd(store)
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
				updateWorker(view, minion, store, etcdData)
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
				etcdData, err = updateEtcd(store, etcdData, containers)
				if err != nil {
					log.WithError(err).Error("Etcd update failed.")
					return nil
				}

				updateLeaderDBC(view, containers, etcdData, ipMap)
			}

			updateDBLabels(view, etcdData, ipMap)
			return nil
		})
	}
}

func makeEtcdDir(dir string, store Store, ttl time.Duration) {
	for {
		err := createEtcdDir(dir, store, ttl)
		if err == nil {
			break
		}

		log.WithError(err).Debug("Failed to create etcd dir")
		time.Sleep(5 * time.Second)
	}
}

func createEtcdDir(dir string, store Store, ttl time.Duration) error {
	err := store.Mkdir(dir, ttl)
	if err == nil {
		return nil
	}

	// If the directory already exists, no need to create it
	etcdErr, ok := err.(client.Error)
	if ok && etcdErr.Code == client.ErrorCodeNodeExist {
		return store.RefreshDir(dir, ttl)
	}

	return err
}

func readEtcd(store Store) (storeData, error) {
	containers, err := store.Get(containerStore)
	labels, err2 := store.Get(labelToIPStore)
	if err2 != nil {
		err = err2
	}

	etcdContainerSlice := []storeContainer{}
	multiHostMap := map[string]string{}

	// Failed store reads will just be skipped by Unmarshal, which is fine
	// since an error is returned
	json.Unmarshal([]byte(containers), &etcdContainerSlice)
	json.Unmarshal([]byte(labels), &multiHostMap)

	return storeData{etcdContainerSlice, multiHostMap}, err
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

func updateEtcd(s Store, etcdData storeData,
	containers []db.Container) (storeData, error) {

	if etcdData, err := updateEtcdContainer(s, etcdData, containers); err != nil {
		return etcdData, err
	}

	if etcdData, err := updateEtcdLabel(s, etcdData, containers); err != nil {
		return etcdData, err
	}

	return etcdData, nil
}

func dbSliceToStoreSlice(dbcs []db.Container) []storeContainer {
	dbContainerSlice := []storeContainer{}
	for _, c := range dbcs {
		sc := storeContainer{
			StitchID: c.StitchID,
			Minion:   c.Minion,
			Image:    c.Image,
			Command:  c.Command,
			Labels:   c.Labels,
			Env:      c.Env,
		}
		dbContainerSlice = append(dbContainerSlice, sc)
	}
	return dbContainerSlice
}

func writeDiff(s Store, data storeData, newData []storeContainer) (storeData, error) {
	return data, nil
}

func updateEtcdContainer(s Store, etcdData storeData,
	containers []db.Container) (storeData, error) {

	dbContainerSlice := dbSliceToStoreSlice(containers)
	sort.Sort(storeContainerSlice(dbContainerSlice))
	newContainers, _ := json.Marshal(dbContainerSlice)
	oldContainers, _ := json.Marshal(etcdData.containers)
	if string(newContainers) == string(oldContainers) {
		return etcdData, nil
	}

	if err := s.Set(containerStore, string(newContainers), 0); err != nil {
		return etcdData, err
	}

	etcdData.containers = dbContainerSlice
	return etcdData, nil

}

func updateEtcdLabel(s Store, etcdData storeData, containers []db.Container) (storeData,
	error) {

	// Collect a map of labels to all of the containers that have that label.
	labelContainers := map[string][]db.Container{}
	for _, c := range containers {
		for _, l := range c.Labels {
			labelContainers[l] = append(labelContainers[l], c)
		}
	}

	newMultiHosts := map[string]string{}

	// Gather the multihost containers and set the IPs of non-multihost containers
	// at the same time. The single host IPs are retrieved from the map of container
	// IPs that updateEtcdDocker created.
	for label, cs := range labelContainers {
		if len(cs) > 1 {
			newMultiHosts[label] = ""
		}
	}

	// Etcd is the source of truth for IPs. If the label exists in both etcd and the
	// db and it is a multihost label, then assign it the IP that etcd has.
	// Otherwise, it stays unassigned and syncLabelIPs will take care of it.
	for id := range newMultiHosts {
		if ip, ok := etcdData.multiHost[id]; ok {
			newMultiHosts[id] = ip
		}
	}

	// No need to sync the SingleHost IPs, since they get their IPs from the dockerIP
	// map, which was already synced in updateEtcdDocker
	syncLabelIPs(newMultiHosts)

	if util.StrStrMapEqual(newMultiHosts, etcdData.multiHost) {
		return etcdData, nil
	}

	newLabelJSON, _ := json.Marshal(newMultiHosts)
	if err := s.Set(labelToIPStore, string(newLabelJSON), 0); err != nil {
		return etcdData, err
	}

	etcdData.multiHost = newMultiHosts
	return etcdData, nil
}

func updateLeaderDBC(view db.Database, dbcs []db.Container,
	etcdData storeData, ipMap map[string]string) {

	for _, dbc := range dbcs {
		ipVal := ipMap[strconv.Itoa(dbc.StitchID)]
		mac := ipdef.IPStrToMac(ipVal)
		if dbc.IP != ipVal || dbc.Mac != mac {
			dbc.IP = ipVal
			dbc.Mac = mac
			view.Commit(dbc)
		}
	}
}

func updateWorker(view db.Database, self db.Minion, store Store,
	etcdData storeData) {

	var containers []storeContainer
	for _, etcdc := range etcdData.containers {
		if etcdc.Minion == self.PrivateIP {
			containers = append(containers, etcdc)
		}
	}

	pairs, dbcs, etcdcs := join.Join(view.SelectFromContainer(nil), containers,
		func(left, right interface{}) int {
			dbc := left.(db.Container)
			l := storeContainer{
				StitchID: dbc.StitchID,
				Minion:   dbc.Minion,
				Image:    dbc.Image,
				Command:  dbc.Command,
				Env:      dbc.Env,
				Labels:   dbc.Labels,
			}
			return containerJoinScore(l, right.(storeContainer))
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
		etcdc := pair.R.(storeContainer)

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

	jsonData, err := json.Marshal(newIPMap)
	if err != nil {
		log.WithError(err).Error("Failed to marshal minion container IP map")
		return
	}

	err = store.Set(path.Join(selfStore, minionIPStore), string(jsonData), 0)
	if err != nil {
		log.WithError(err).Error("Failed to update minion container IP map")
	}
}

func updateDBLabels(view db.Database, etcdData storeData, ipMap map[string]string) {
	// Gather all of the label keys and IPs for single host labels, and IPs of
	// the containers in a given label.
	containerIPs := map[string][]string{}
	labelIPs := map[string]string{}
	labelKeys := map[string]struct{}{}
	for _, c := range etcdData.containers {
		for _, l := range c.Labels {
			labelKeys[l] = struct{}{}
			cIP := ipMap[strconv.Itoa(c.StitchID)]
			if _, ok := etcdData.multiHost[l]; !ok {
				labelIPs[l] = cIP
			}

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
		if _, ok := etcdData.multiHost[dbl.Label]; ok {
			dbl.IP = etcdData.multiHost[dbl.Label]
			dbl.MultiHost = true
		} else {
			dbl.IP = labelIPs[dbl.Label]
			dbl.MultiHost = false
		}
		dbl.ContainerIPs = containerIPs[dbl.Label]

		view.Commit(dbl)
	}
}

// syncLabelIPs takes a map of IDs to IPs and creates an IP address for every entry
// that's missing one.
func syncLabelIPs(ipMap map[string]string) {
	var unassigned []string
	ipSet := map[string]struct{}{ipdef.GatewayIP.String(): {}}
	for k, ipString := range ipMap {
		_, duplicate := ipSet[ipString]
		if duplicate || !ipdef.LabelSubnet.Contains(net.ParseIP(ipString)) {
			unassigned = append(unassigned, k)
			continue
		}
		ipSet[ipString] = struct{}{}
	}

	for _, k := range unassigned {
		ip, err := allocateIP(ipSet, ipdef.LabelSubnet)
		if err != nil {
			log.WithError(err).Errorf("Failed to allocate IP for %s.", k)
			ipMap[k] = ""
			continue
		}

		ipMap[k] = ip
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

func containerJoinScore(left, right storeContainer) int {
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

func (cs storeContainerSlice) Len() int {
	return len(cs)
}

func (cs storeContainerSlice) Less(i, j int) bool {
	return cs[i].StitchID < cs[j].StitchID
}

func (cs storeContainerSlice) Swap(i, j int) {
	cs[i], cs[j] = cs[j], cs[i]
}
