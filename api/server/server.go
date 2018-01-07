package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/api/pb"
	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud"
	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/vault"
	"github.com/kelda/kelda/version"

	"github.com/docker/distribution/reference"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

var errDaemonOnlyRPC = errors.New("only defined on the daemon")

type server struct {
	conn db.Conn

	// The API server runs in two locations:  on minions in the cluster, and on
	// the daemon. When the server is running on the daemon, we automatically
	// proxy certain Queries to the cluster because the daemon doesn't track
	// those tables (e.g. Container, Connection, LoadBalancer).
	runningOnDaemon bool

	// The credentials to use while connecting to clients in the cluster.
	clientCreds connection.Credentials
}

// Run starts a server that responds to connections from the CLI. It runs on both
// the daemon and on the minion. The server provides various client-relevant
// methods, such as starting deployments, and querying the state of the system.
// This is in contrast to the minion server (minion/pb/pb.proto), which facilitates
// the actual deployment.
func Run(conn db.Conn, listenAddr string, runningOnDaemon bool,
	creds connection.Credentials) error {
	proto, addr, err := api.ParseListenAddress(listenAddr)
	if err != nil {
		return err
	}

	sock, s := connection.Server(proto, addr, creds.ServerOpts())

	// Cleanup the socket if we're interrupted.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func(c chan os.Signal) {
		sig := <-c
		var activeMachinesCount = len(conn.SelectFromMachine(nil))
		var namespace, _ = conn.GetBlueprintNamespace()
		if activeMachinesCount > 0 {
			log.Warnf("\n%d machines will continue running after the Kelda"+
				" daemon shuts down. If you'd like to stop them, restart"+
				" the daemon and run `kelda stop %s`.\n",
				activeMachinesCount, namespace)
		}
		log.Printf("Caught signal %s: shutting down.\n", sig)
		sock.Close()
		os.Exit(0)
	}(sigc)

	apiServer := server{conn, runningOnDaemon, creds}
	pb.RegisterAPIServer(s, apiServer)
	s.Serve(sock)

	return nil
}

func (s server) SetSecret(ctx context.Context, msg *pb.Secret) (*pb.SecretReply, error) {
	// If this method is called while running on the daemon, forward the secret
	// assignment to the leader. The assignment is synchronous, so the user
	// will get immediate feedback on whether or not the secret was successfully
	// set.
	if s.runningOnDaemon {
		machines := s.conn.SelectFromMachine(nil)
		leaderClient, err := newLeaderClient(machines, s.clientCreds)
		if err != nil {
			return &pb.SecretReply{}, err
		}
		defer leaderClient.Close()
		return &pb.SecretReply{}, leaderClient.SetSecret(msg.Name, msg.Value)
	}

	// We're running in the cluster, so write the secret into Vault.
	client, err := newVaultClient(s.conn.MinionSelf().PrivateIP)
	if err != nil {
		return &pb.SecretReply{}, err
	}

	return &pb.SecretReply{}, client.Write(msg.Name, msg.Value)
}

// Query runs in two modes: daemon, or local. If in local mode, Query simply
// returns the requested table from its local database. If in daemon mode,
// Query proxies certain table requests (e.g. Container and Connection) to the
// cluster. This is necessary because some tables are only used on the minions,
// and aren't synced back to the daemon.
func (s server) Query(cts context.Context, query *pb.DBQuery) (*pb.QueryReply, error) {
	var rows interface{}
	var err error

	table := db.TableType(query.Table)
	if s.runningOnDaemon {
		rows, err = s.queryFromDaemon(table)
	} else {
		rows, err = s.queryLocal(table)
	}

	if err != nil {
		return nil, err
	}

	json, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}

	return &pb.QueryReply{TableContents: string(json)}, nil
}

func (s server) queryLocal(table db.TableType) (interface{}, error) {
	switch table {
	case db.MachineTable:
		return s.conn.SelectFromMachine(nil), nil
	case db.ContainerTable:
		return s.conn.SelectFromContainer(nil), nil
	case db.EtcdTable:
		return s.conn.SelectFromEtcd(nil), nil
	case db.ConnectionTable:
		return s.conn.SelectFromConnection(nil), nil
	case db.LoadBalancerTable:
		return s.conn.SelectFromLoadBalancer(nil), nil
	case db.BlueprintTable:
		return s.conn.SelectFromBlueprint(nil), nil
	case db.ImageTable:
		return s.conn.SelectFromImage(nil), nil
	default:
		return nil, fmt.Errorf("unrecognized table: %s", table)
	}
}

func (s server) queryFromDaemon(table db.TableType) (
	interface{}, error) {

	switch table {
	case db.MachineTable, db.BlueprintTable:
		return s.queryLocal(table)
	}

	var leaderClient client.Client
	leaderClient, err := newLeaderClient(s.conn.SelectFromMachine(nil), s.clientCreds)
	if err != nil {
		return nil, err
	}
	defer leaderClient.Close()

	switch table {
	case db.ContainerTable:
		return s.getClusterContainers(leaderClient)
	case db.ConnectionTable:
		return leaderClient.QueryConnections()
	case db.LoadBalancerTable:
		return leaderClient.QueryLoadBalancers()
	case db.ImageTable:
		return leaderClient.QueryImages()
	default:
		return nil, fmt.Errorf("unrecognized table: %s", table)
	}
}

func (s server) QueryMinionCounters(ctx context.Context, in *pb.MinionCountersRequest) (
	*pb.CountersReply, error) {
	if !s.runningOnDaemon {
		return nil, errDaemonOnlyRPC
	}

	clnt, err := newClient(api.RemoteAddress(in.Host), s.clientCreds)
	if err != nil {
		return nil, err
	}

	counters, err := clnt.QueryCounters()
	if err != nil {
		return nil, err
	}

	reply := &pb.CountersReply{}
	for i := range counters {
		reply.Counters = append(reply.Counters, &counters[i])
	}
	return reply, nil
}

func (s server) QueryCounters(ctx context.Context, in *pb.CountersRequest) (
	*pb.CountersReply, error) {
	return &pb.CountersReply{Counters: counter.Dump()}, nil
}

func (s server) Deploy(cts context.Context, deployReq *pb.DeployRequest) (
	*pb.DeployReply, error) {

	if !s.runningOnDaemon {
		return nil, errDaemonOnlyRPC
	}

	newBlueprint, err := blueprint.FromJSON(deployReq.Deployment)
	if err != nil {
		return &pb.DeployReply{}, err
	}

	for _, c := range newBlueprint.Containers {
		if _, err := reference.ParseAnyReference(c.Image.Name); err != nil {
			return &pb.DeployReply{}, fmt.Errorf("could not parse "+
				"container image %s: %s", c.Image.Name, err.Error())
		}
	}

	// Ensure that the region is valid
	if len(newBlueprint.Machines) > 0 {
		// Since the Javascript code ensures that all machines have the same
		// region and provider, we only need to check the region of the first
		// machine is valid for its provider.
		first := newBlueprint.Machines[0]
		regionValid := false
		for _, r := range cloud.ValidRegions(db.ProviderName(first.Provider)) {
			if r == first.Region {
				regionValid = true
			}
		}
		if !regionValid {
			return &pb.DeployReply{}, fmt.Errorf("region: %s is "+
				"not supported for provider: %s", first.Region,
				first.Provider)
		}
	}

	s.conn.Txn(db.BlueprintTable, db.MachineTable).Run(func(view db.Database) error {
		bp, err := view.GetBlueprint()
		if err != nil {
			bp = view.InsertBlueprint()
		} else {
			// If the namespace changed, remove all the machines from the
			// database since those applied to the old namespace, and leaving
			// them in the database will cause incorrect behavior such as
			// sending the new blueprint to the old machines.
			if bp.Namespace != newBlueprint.Namespace {
				for _, dbm := range view.SelectFromMachine(nil) {
					view.Remove(dbm)
				}
			}
		}

		bp.Blueprint = newBlueprint
		view.Commit(bp)
		return nil
	})

	// XXX: Remove this error when the Vagrant provider is done.
	for _, machine := range newBlueprint.Machines {
		if machine.Provider == string(db.Vagrant) {
			err = errors.New("The Vagrant provider is still in development." +
				" The blueprint will continue to run, but" +
				" there may be some errors.")
			return &pb.DeployReply{}, err
		}
	}

	return &pb.DeployReply{}, nil
}

func (s server) Version(_ context.Context, _ *pb.VersionRequest) (
	*pb.VersionReply, error) {
	return &pb.VersionReply{Version: version.Version}, nil
}

func (s server) getClusterContainers(leaderClient client.Client) (interface{}, error) {
	leaderContainers, err := leaderClient.QueryContainers()
	if err != nil {
		return nil, err
	}

	workerContainers, err := queryWorkers(s.conn.SelectFromMachine(nil),
		s.clientCreds)
	if err != nil {
		return nil, err
	}

	return updateLeaderContainerAttrs(leaderContainers, workerContainers), nil
}

type queryContainersResponse struct {
	containers []db.Container
	err        error
}

// queryWorkers gets a client for all worker machines and returns a list of
// `db.Container`s on these machines.
func queryWorkers(machines []db.Machine, creds connection.Credentials) (
	[]db.Container, error) {

	var wg sync.WaitGroup
	queryResponses := make(chan queryContainersResponse, len(machines))
	for _, m := range machines {
		if m.PublicIP == "" || m.Role != db.Worker || m.Status != db.Connected {
			continue
		}

		wg.Add(1)
		go func(m db.Machine) {
			defer wg.Done()
			var qContainers []db.Container
			client, err := newClient(api.RemoteAddress(m.PublicIP), creds)
			if err == nil {
				defer client.Close()
				qContainers, err = client.QueryContainers()
			}
			queryResponses <- queryContainersResponse{qContainers, err}
		}(m)
	}

	wg.Wait()
	close(queryResponses)

	var containers []db.Container
	for resp := range queryResponses {
		if resp.err != nil {
			return nil, resp.err
		}
		containers = append(containers, resp.containers...)
	}
	return containers, nil
}

// updateLeaderContainerAttrs updates the containers described by the leader with
// the worker-only attributes.
func updateLeaderContainerAttrs(lContainers []db.Container, wContainers []db.Container) (
	allContainers []db.Container) {

	// Map BlueprintID to db.Container for a hash join.
	cMap := make(map[string]db.Container)
	for _, wc := range wContainers {
		cMap[wc.BlueprintID] = wc
	}

	// If we are able to match a worker container to a leader container, then we
	// copy the worker-only attributes to the leader view.
	for _, lc := range lContainers {
		if wc, ok := cMap[lc.BlueprintID]; ok {
			lc.Created = wc.Created
			lc.DockerID = wc.DockerID
			lc.Status = wc.Status
		}
		allContainers = append(allContainers, lc)
	}
	return allContainers
}

// client.New, client.Leader, and vault.New are saved in variables to
// facilitate injecting test clients for unit testing.
var newClient = client.New
var newLeaderClient = client.Leader
var newVaultClient = vault.New
