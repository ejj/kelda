package supervisor

import (
	"net"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
)

type testCtx struct {
	fd    fakeDocker
	execs [][]string

	conn db.Conn
}

func initTest() *testCtx {
	conn = db.New()
	md, _dk := docker.NewMock()
	ctx := testCtx{fakeDocker{_dk, md}, nil, conn}
	dk = ctx.fd.Client

	ctx.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMinion()
		m.Self = true
		view.Commit(m)
		e := view.InsertEtcd()
		view.Commit(e)
		return nil
	})

	execRun = func(name string, args ...string) ([]byte, error) {
		ctx.execs = append(ctx.execs, append([]string{name}, args...))
		return nil, nil
	}

	cfgGateway = func(name string, ip net.IPNet) error {
		execRun("cfgGateway", ip.String())
		return nil
	}

	cfgOVN = func(myIP, leaderIP string) error {
		execRun("cfgOvn", myIP, leaderIP)
		return nil
	}

	return &ctx
}

type fakeDocker struct {
	docker.Client
	md *docker.MockClient
}

func (f fakeDocker) running() map[string][]string {
	containers, _ := f.List(nil)

	res := map[string][]string{}
	for _, c := range containers {
		res[c.Name] = c.Args
	}
	return res
}
