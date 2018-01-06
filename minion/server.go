package minion

import (
	"sort"
	"strings"

	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/pb"

	"golang.org/x/net/context"

	log "github.com/sirupsen/logrus"
)

type server struct {
	db.Conn
}

func minionServerRun(conn db.Conn, creds connection.Credentials) {
	sock, s := connection.Server("tcp", ":9999", creds.ServerOpts())
	server := server{conn}
	pb.RegisterMinionServer(s, server)
	s.Serve(sock)
}

func (s server) GetMinionConfig(cts context.Context,
	_ *pb.Request) (*pb.MinionConfig, error) {

	var cfg pb.MinionConfig

	c.Inc("GetMinionConfig")

	m := s.MinionSelf()
	cfg.Role = db.RoleToPB(m.Role)
	cfg.PrivateIP = m.PrivateIP
	cfg.Blueprint = m.Blueprint
	cfg.Provider = m.Provider
	cfg.Size = m.Size
	cfg.Region = m.Region
	cfg.AuthorizedKeys = strings.Split(m.AuthorizedKeys, "\n")

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

	c.Inc("SetMinionConfig")
	go s.Txn(db.EtcdTable, db.MinionTable).Run(func(view db.Database) error {
		minion := view.MinionSelf()
		minion.PrivateIP = msg.PrivateIP
		minion.Blueprint = msg.Blueprint
		minion.Provider = msg.Provider
		minion.Size = msg.Size
		minion.Region = msg.Region
		minion.FloatingIP = msg.FloatingIP
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
