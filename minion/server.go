package minion

import (
	"net"
	"sort"
	"strings"
	"time"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/pb"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/Sirupsen/logrus"
)

type server struct {
	db.Conn
}

func minionServerRun(conn db.Conn) {
	var sock net.Listener
	server := server{conn}
	for {
		var err error
		sock, err = net.Listen("tcp", ":9999")
		if err != nil {
			log.WithError(err).Error("Failed to open socket.")
		} else {
			break
		}

		time.Sleep(30 * time.Second)
	}

	s := grpc.NewServer()
	pb.RegisterMinionServer(s, server)
	s.Serve(sock)
}

func (s server) GetMinionConfig(cts context.Context,
	_ *pb.Request) (*pb.MinionConfig, error) {

	var cfg pb.MinionConfig

	if m, err := s.MinionSelf(); err == nil {
		cfg.Role = db.RoleToPB(m.Role)
		cfg.PrivateIP = m.PrivateIP
		cfg.Spec = m.Spec
		cfg.Provider = m.Provider
		cfg.Size = m.Size
		cfg.Region = m.Region
		cfg.AuthorizedKeys = strings.Split(m.AuthorizedKeys, "\n")
	} else {
		cfg.Role = db.RoleToPB(db.None)
	}

	s.Txn(db.EtcdTable).Run(func(view db.Database) error {
		if etcdRow, err := view.GetEtcd(); err == nil {
			cfg.EtcdMembers = etcdRow.EtcdIPs
		}
		return nil
	})

	return &cfg, nil
}

func (s server) SetMinionConfig(ctx context.Context,
	msg *pb.MinionConfig) (*pb.Reply, error) {
	go s.Txn(db.EtcdTable,
		db.MinionTable).Run(func(view db.Database) error {

		minion, err := view.MinionSelf()
		if err != nil {
			log.Info("Received initial configuation.")
			minion = view.InsertMinion()
		}

		minion.Role = db.PBToRole(msg.Role)
		minion.PrivateIP = msg.PrivateIP
		minion.Spec = msg.Spec
		minion.Provider = msg.Provider
		minion.Size = msg.Size
		minion.Region = msg.Region
		minion.AuthorizedKeys = strings.Join(msg.AuthorizedKeys, "\n")
		minion.Self = true
		view.Commit(minion)

		etcdRow, err := view.GetEtcd()
		if err != nil {
			log.Info("Received boot etcd request.")
			etcdRow = view.InsertEtcd()
		}

		etcdRow.EtcdIPs = msg.EtcdMembers
		sort.Strings(etcdRow.EtcdIPs)
		view.Commit(etcdRow)

		return nil
	})

	return &pb.Reply{}, nil
}
