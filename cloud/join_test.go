package cloud

import (
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	"github.com/stretchr/testify/assert"
)

func TestSyncDB(t *testing.T) {
	checkSyncDB := func(cloudMachines []db.Machine,
		databaseMachines []db.Machine, expected syncDBResult) syncDBResult {
		dbRes := syncDB(cloudMachines, databaseMachines)

		assert.Equal(t, expected.boot, dbRes.boot, "boot")
		assert.Equal(t, expected.stop, dbRes.stop, "stop")
		assert.Equal(t, expected.updateIPs, dbRes.updateIPs, "updateIPs")

		return dbRes
	}

	var noMachines []db.Machine
	dbNoSize := db.Machine{Provider: FakeAmazon, Region: testRegion}
	cmNoSize := db.Machine{Provider: FakeAmazon, Region: testRegion}
	dbLarge := db.Machine{Provider: FakeAmazon, Size: "m4.large", Region: testRegion}
	cmLarge := db.Machine{
		Provider: FakeAmazon,
		Region:   testRegion,
		Size:     "m4.large",
	}

	dbMaster := db.Machine{Provider: FakeAmazon, Role: db.Master}
	cmMasterList := db.Machine{Provider: FakeAmazon, Role: db.Master}
	dbWorker := db.Machine{Provider: FakeAmazon, Role: db.Worker}
	cmWorkerList := db.Machine{Provider: FakeAmazon, Role: db.Worker}

	cmNoIP := db.Machine{Provider: FakeAmazon, CloudID: "id"}
	cmWithIP := db.Machine{
		Provider:   FakeAmazon,
		CloudID:    "id",
		FloatingIP: "ip",
	}
	dbNoIP := db.Machine{Provider: FakeAmazon, CloudID: "id"}
	dbWithIP := db.Machine{Provider: FakeAmazon, CloudID: "id", FloatingIP: "ip"}

	// Test boot with no size
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbNoSize}, syncDBResult{
		boot: []db.Machine{dbNoSize, dbNoSize},
	})

	// Test boot with size
	checkSyncDB(noMachines, []db.Machine{dbLarge, dbLarge}, syncDBResult{
		boot: []db.Machine{dbLarge, dbLarge},
	})

	// Test mixed boot
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbLarge}, syncDBResult{
		boot: []db.Machine{dbNoSize, dbLarge},
	})

	// Test partial boot
	checkSyncDB([]db.Machine{cmNoSize}, []db.Machine{dbNoSize, dbLarge},
		syncDBResult{
			boot: []db.Machine{dbLarge},
		},
	)

	// Test stop
	checkSyncDB([]db.Machine{cmNoSize, cmNoSize}, []db.Machine{}, syncDBResult{
		stop: []db.Machine{cmNoSize, cmNoSize},
	})

	// Test partial stop
	checkSyncDB([]db.Machine{cmNoSize, cmLarge}, []db.Machine{}, syncDBResult{
		stop: []db.Machine{cmNoSize, cmLarge},
	})

	// Test assign Floating IP
	checkSyncDB([]db.Machine{cmNoIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []db.Machine{cmWithIP},
	})

	// Test remove Floating IP
	checkSyncDB([]db.Machine{cmWithIP}, []db.Machine{dbNoIP}, syncDBResult{
		updateIPs: []db.Machine{cmNoIP},
	})

	// Test replace Floating IP
	cNewIP := db.Machine{
		Provider:   FakeAmazon,
		CloudID:    "id",
		FloatingIP: "ip^",
	}
	checkSyncDB([]db.Machine{cNewIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []db.Machine{cmWithIP},
	})

	// Test bad disk size
	checkSyncDB([]db.Machine{{DiskSize: 3}},
		[]db.Machine{{DiskSize: 4}},
		syncDBResult{
			stop: []db.Machine{{DiskSize: 3}},
			boot: []db.Machine{{DiskSize: 4}},
		})

	// Test different roles
	checkSyncDB([]db.Machine{cmWorkerList}, []db.Machine{dbMaster}, syncDBResult{
		boot: []db.Machine{dbMaster},
		stop: []db.Machine{cmWorkerList},
	})

	checkSyncDB([]db.Machine{cmMasterList}, []db.Machine{dbWorker}, syncDBResult{
		boot: []db.Machine{dbWorker},
		stop: []db.Machine{cmMasterList},
	})

	// Test reserved instances.
	checkSyncDB([]db.Machine{{Preemptible: true}},
		[]db.Machine{{Preemptible: false}},
		syncDBResult{
			boot: []db.Machine{{Preemptible: false}},
			stop: []db.Machine{{Preemptible: true}},
		})

	// Test matching role as priority over PublicIP
	dbMaster.PublicIP = "worker"
	cmMasterList.PublicIP = "master"
	dbWorker.PublicIP = "master"
	cmWorkerList.PublicIP = "worker"

	checkSyncDB([]db.Machine{cmMasterList, cmWorkerList},
		[]db.Machine{dbMaster, dbWorker},
		syncDBResult{})

	// Test shuffling roles before CloudID is assigned
	dbw1 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w1"}
	dbw2 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w2"}
	dbw3 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w3"}

	mw1 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw1", PublicIP: "w1"}
	mw2 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw2", PublicIP: "w2"}
	mw3 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw3", PublicIP: "w3"}

	pair1 := join.Pair{L: dbw1, R: mw1}
	pair2 := join.Pair{L: dbw2, R: mw2}
	pair3 := join.Pair{L: dbw3, R: mw3}

	exp := []join.Pair{
		pair1,
		pair2,
		pair3,
	}

	pairs := checkSyncDB([]db.Machine{mw1, mw2, mw3},
		[]db.Machine{dbw1, dbw2, dbw3},
		syncDBResult{})

	assert.Equal(t, exp, pairs.pairs)

	// Test FloatingIP without role
	dbf1 := db.Machine{Provider: FakeAmazon, Role: db.Master, PublicIP: "master"}
	dbf2 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "worker",
		FloatingIP: "float"}

	cmf1 := db.Machine{Provider: FakeAmazon, PublicIP: "worker", CloudID: "worker"}
	cmf2 := db.Machine{Provider: FakeAmazon, PublicIP: "master", CloudID: "master"}

	// No roles, CloudIDs not assigned, so nothing should happen
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{})

	cmf1.Role = db.Worker

	// One role assigned, so one CloudID to be assigned after
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{})

	dbf2.CloudID = cmf1.CloudID
	cmf2.Role = db.Master

	// Now that CloudID of machine with FloatingIP has been assigned,
	// FloatingIP should also be assigned
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{
			updateIPs: []db.Machine{
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "worker",
					CloudID:    "worker",
					FloatingIP: "float",
				},
			},
		})

	// Test FloatingIP role shuffling
	dbm2 := db.Machine{Provider: FakeAmazon, Role: db.Master, PublicIP: "mIP"}
	dbm3 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "wIP1",
		FloatingIP: "flip1"}
	dbm4 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "wIP2",
		FloatingIP: "flip2"}

	m2 := db.Machine{Provider: FakeAmazon, PublicIP: "mIP", CloudID: "m2"}
	m3 := db.Machine{Provider: FakeAmazon, PublicIP: "wIP1", CloudID: "m3"}
	m4 := db.Machine{Provider: FakeAmazon, PublicIP: "wIP2", CloudID: "m4"}

	m2.Role = db.Worker
	m3.Role = db.Master
	m4.Role = db.Worker

	// CloudIDs not assigned to db machines yet, so shouldn't update anything.
	checkSyncDB([]db.Machine{m2, m3, m4},
		[]db.Machine{dbm2, dbm3, dbm4},
		syncDBResult{})

	dbm2.CloudID = m3.CloudID
	dbm3.CloudID = m2.CloudID
	dbm4.CloudID = m4.CloudID

	// CloudIDs are now assigned, so time to update floating IPs
	checkSyncDB([]db.Machine{m2, m3, m4},
		[]db.Machine{dbm2, dbm3, dbm4},
		syncDBResult{
			updateIPs: []db.Machine{
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "mIP",
					CloudID:    "m2",
					FloatingIP: "flip1",
				},
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "wIP2",
					CloudID:    "m4",
					FloatingIP: "flip2",
				},
			},
		})

}

func TestGetACLs(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	exp := map[acl.ACL]struct{}{
		{CidrIP: "local", MinPort: 1, MaxPort: 65535}: {},
	}

	// Empty blueprint should have "local" added to it.
	acls := cld.getACLs(db.Blueprint{})
	assert.Equal(t, exp, acls)

	// A blueprint with local, shouldn't have it added a second time.
	acls = cld.getACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{AdminACL: []string{"local"}},
	})
	assert.Equal(t, exp, acls)

	// Connections that aren't to or from public, shouldn't affect the acls.
	acls = cld.getACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{
			Connections: []blueprint.Connection{{
				From:    "foo",
				To:      "bar",
				MinPort: 5,
				MaxPort: 6,
			}},
		},
	})
	assert.Equal(t, exp, acls)

	// Connections from public create an ACL.
	acls = cld.getACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{
			Connections: []blueprint.Connection{{
				From:    blueprint.PublicInternetLabel,
				To:      "bar",
				MinPort: 1,
				MaxPort: 2,
			}},
		},
	})
	exp[acl.ACL{CidrIP: "0.0.0.0/0", MinPort: 1, MaxPort: 2}] = struct{}{}
	assert.Equal(t, exp, acls)
}
