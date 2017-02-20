package engine

import (
	"errors"
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/stitch"
	"github.com/stretchr/testify/assert"
)

func TestEngine(t *testing.T) {
	pre := `var deployment = createDeployment({
		namespace: "namespace",
		adminACL: ["1.2.3.4/32"],
	});
	var baseMachine = new Machine({provider: "Amazon", size: "m4.large"});`
	conn := db.New()

	code := pre + `deployment.deploy(baseMachine.asMaster().replicate(2));
		deployment.deploy(baseMachine.asWorker().replicate(3));`

	updateStitch(t, conn, prog(t, code))
	acl, err := selectACL(conn)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(acl.Admin))

	masters, workers := selectMachines(conn)
	assert.Equal(t, 2, len(masters))
	assert.Equal(t, 3, len(workers))

	/* Verify master increase. */
	code = pre + `deployment.deploy(baseMachine.asMaster().replicate(4));
		deployment.deploy(baseMachine.asWorker().replicate(5));`

	updateStitch(t, conn, prog(t, code))
	masters, workers = selectMachines(conn)
	assert.Equal(t, 4, len(masters))
	assert.Equal(t, 5, len(workers))

	/* Verify that external writes stick around. */
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		masters := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Master
		})
		workers := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Worker
		})

		for _, master := range masters {
			master.CloudID = "1"
			master.PublicIP = "2"
			master.PrivateIP = "3"
			view.Commit(master)
		}

		for _, worker := range workers {
			worker.CloudID = "1"
			worker.PublicIP = "2"
			worker.PrivateIP = "3"
			view.Commit(worker)
		}

		return nil
	})

	/* Also verify that masters and workers decrease properly. */
	code = pre + `deployment.deploy(baseMachine.asMaster());
		deployment.deploy(baseMachine.asWorker());`
	updateStitch(t, conn, prog(t, code))

	masters, workers = selectMachines(conn)

	assert.Equal(t, 1, len(masters))
	assert.Equal(t, "1", masters[0].CloudID)
	assert.Equal(t, "2", masters[0].PublicIP)
	assert.Equal(t, "3", masters[0].PrivateIP)

	assert.Equal(t, 1, len(workers))
	assert.Equal(t, "1", workers[0].CloudID)
	assert.Equal(t, "2", workers[0].PublicIP)
	assert.Equal(t, "3", workers[0].PrivateIP)

	/* Empty Namespace does nothing. */
	code = pre + `deployment.namespace = "";
		deployment.deploy(baseMachine.asMaster());
		deployment.deploy(baseMachine.asWorker());`
	updateStitch(t, conn, prog(t, code))
	masters, workers = selectMachines(conn)

	assert.Equal(t, 1, len(masters))
	assert.Equal(t, "1", masters[0].CloudID)
	assert.Equal(t, "2", masters[0].PublicIP)
	assert.Equal(t, "3", masters[0].PrivateIP)

	assert.Equal(t, 1, len(workers))
	assert.Equal(t, "1", workers[0].CloudID)
	assert.Equal(t, "2", workers[0].PublicIP)
	assert.Equal(t, "3", workers[0].PrivateIP)

	/* Verify things go to zero. */
	code = pre + `deployment.deploy(baseMachine.asWorker())`
	updateStitch(t, conn, prog(t, code))
	masters, workers = selectMachines(conn)
	assert.Zero(t, len(masters))
	assert.Zero(t, len(workers))

	// This function checks whether there is a one-to-one mapping for each machine
	// in `slice` to a provider in `providers`.
	providersInSlice := func(slice db.MachineSlice, providers db.ProviderSlice) bool {
		lKey := func(left interface{}) interface{} {
			return left.(db.Machine).Provider
		}
		rKey := func(right interface{}) interface{} {
			return right.(db.Provider)
		}
		_, l, r := join.HashJoin(slice, providers, lKey, rKey)
		return len(l) == 0 && len(r) == 0
	}

	/* Test mixed providers. */
	code = `deployment.deploy([
		new Machine({provider: "Amazon", size: "m4.large", role: "Master"}),
		new Machine({provider: "Vagrant", size: "v.large", role: "Master"}),
		new Machine({provider: "Amazon", size: "m4.large", role: "Worker"}),
		new Machine({provider: "Google", size: "g.large", role: "Worker"})]);`
	updateStitch(t, conn, prog(t, code))
	masters, workers = selectMachines(conn)
	assert.True(t, providersInSlice(masters,
		db.ProviderSlice{db.Amazon, db.Vagrant}))
	assert.True(t, providersInSlice(workers, db.ProviderSlice{db.Amazon, db.Google}))

	/* Test that machines with different providers don't match. */
	code = `deployment.deploy([
		new Machine({provider: "Amazon", size: "m4.large", role: "Master"}),
		new Machine({provider: "Amazon", size: "m4.large", role: "Worker"})]);`
	updateStitch(t, conn, prog(t, code))
	masters, _ = selectMachines(conn)
	assert.True(t, providersInSlice(masters, db.ProviderSlice{db.Amazon}))
}

func TestSort(t *testing.T) {
	pre := `var baseMachine = new Machine({provider: "Amazon", size: "m4.large"});`
	conn := db.New()

	updateStitch(t, conn, prog(t, pre+`
	deployment.deploy(baseMachine.asMaster().replicate(3));
	deployment.deploy(baseMachine.asWorker().replicate(1));`))
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Master
		})
		assert.Equal(t, 3, len(machines))

		machines[0].StitchID = ""
		view.Commit(machines[0])

		machines[2].StitchID = ""
		machines[2].PublicIP = "a"
		machines[2].PrivateIP = "b"
		view.Commit(machines[2])

		machines[1].StitchID = ""
		machines[1].PrivateIP = "c"
		view.Commit(machines[1])

		return nil
	})

	updateStitch(t, conn, prog(t, pre+`
	deployment.deploy(baseMachine.asMaster().replicate(2));
	deployment.deploy(baseMachine.asWorker().replicate(1));`))
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Master
		})
		assert.Equal(t, 2, len(machines))

		for _, m := range machines {
			assert.False(t, m.PublicIP == "" && m.PrivateIP == "")
		}

		return nil
	})
}

func TestACLs(t *testing.T) {
	conn := db.New()

	code := `createDeployment({adminACL: ["1.2.3.4/32", "local"]})
		.deploy([
			new Machine({provider: "Amazon", role: "Master"}),
			new Machine({provider: "Amazon", role: "Worker"})
		]);`

	myIP = func() (string, error) {
		return "5.6.7.8", nil
	}
	updateStitch(t, conn, prog(t, code))
	acl, err := selectACL(conn)
	assert.Nil(t, err)
	assert.Equal(t, []string{"1.2.3.4/32", "5.6.7.8/32"}, acl.Admin)

	myIP = func() (string, error) {
		return "", errors.New("")
	}
	updateStitch(t, conn, prog(t, code))
	acl, err = selectACL(conn)
	assert.Nil(t, err)
	assert.Equal(t, []string{"1.2.3.4/32"}, acl.Admin)
}

func prog(t *testing.T, code string) stitch.Stitch {
	result, err := stitch.FromJavascript(code, stitch.DefaultImportGetter)
	if err != nil {
		t.Error(err.Error())
		return stitch.Stitch{}
	}

	return result
}

func selectMachines(conn db.Conn) (masters, workers []db.Machine) {
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		masters = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Master
		})
		workers = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Worker
		})
		return nil
	})
	return
}

func selectACL(conn db.Conn) (acl db.ACL, err error) {
	err = conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		acl, err = view.GetACL()
		return err
	})
	return
}

func updateStitch(t *testing.T, conn db.Conn, stitch stitch.Stitch) {
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		cluster, err := view.GetCluster()
		if err != nil {
			cluster = view.InsertCluster()
		}
		cluster.Spec = stitch.String()
		view.Commit(cluster)
		return nil
	})
	assert.Nil(t, conn.Txn(db.AllTables...).Run(updateTxn))
}
