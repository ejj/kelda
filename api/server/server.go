package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/pb"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/stitch"

	"github.com/docker/distribution/reference"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
)

type server struct {
	conn db.Conn
}

// Run accepts incoming `quiltctl` connections and responds to them.
func Run(conn db.Conn, listenAddr string) error {
	proto, addr, err := api.ParseListenAddress(listenAddr)
	if err != nil {
		return err
	}

	var sock net.Listener
	apiServer := server{conn}
	for {
		sock, err = net.Listen(proto, addr)

		if err == nil {
			break
		}
		log.WithError(err).Error("Failed to open socket.")

		time.Sleep(30 * time.Second)
	}

	// Cleanup the socket if we're interrupted.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func(c chan os.Signal) {
		sig := <-c
		log.Printf("Caught signal %s: shutting down.\n", sig)
		sock.Close()
		os.Exit(0)
	}(sigc)

	s := grpc.NewServer()
	pb.RegisterAPIServer(s, apiServer)
	s.Serve(sock)

	return nil
}

func (s server) Query(cts context.Context, query *pb.DBQuery) (*pb.QueryReply, error) {
	var rows interface{}
	switch db.TableType(query.Table) {
	case db.MachineTable:
		rows = s.conn.SelectFromMachine(nil)
	case db.ContainerTable:
		rows = s.conn.SelectFromContainer(nil)
	case db.EtcdTable:
		rows = s.conn.SelectFromEtcd(nil)
	case db.ConnectionTable:
		rows = s.conn.SelectFromConnection(nil)
	case db.LabelTable:
		rows = s.conn.SelectFromLabel(nil)
	case db.ClusterTable:
		rows = s.conn.SelectFromCluster(nil)
	default:
		return nil, fmt.Errorf("unrecognized table: %s", query.Table)
	}

	json, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}

	return &pb.QueryReply{TableContents: string(json)}, nil
}

func (s server) Deploy(cts context.Context, deployReq *pb.DeployRequest) (
	*pb.DeployReply, error) {

	stitch, err := stitch.FromJSON(deployReq.Deployment)
	if err != nil {
		return &pb.DeployReply{}, err
	}

	for _, c := range stitch.Containers {
		if _, err := reference.ParseAnyReference(c.Image); err != nil {
			return &pb.DeployReply{}, fmt.Errorf("could not parse "+
				"container image %s: %s", c.Image, err.Error())
		}
	}

	err = s.conn.Txn(db.ClusterTable).Run(func(view db.Database) error {
		cluster, err := view.GetCluster()
		if err != nil {
			cluster = view.InsertCluster()
		}

		cluster.Spec = stitch.String()
		view.Commit(cluster)
		return nil
	})
	if err != nil {
		return &pb.DeployReply{}, err
	}

	// XXX: Remove this error when the Vagrant provider is done.
	for _, machine := range stitch.Machines {
		if machine.Provider == db.Vagrant {
			err = errors.New("The Vagrant provider is in development." +
				" The stitch will continue to run, but" +
				" probably won't work correctly.")
			return &pb.DeployReply{}, err
		}
	}

	return &pb.DeployReply{}, nil
}
