package minion

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/pb"
)

func TestSetMinionConfig(t *testing.T) {
	t.Parallel()
	s := server{db.New()}

	cfg := pb.MinionConfig{
		Role:           pb.MinionConfig_MASTER,
		PrivateIP:      "priv",
		Spec:           "spec",
		Provider:       "provider",
		Size:           "size",
		Region:         "region",
		EtcdMembers:    []string{"etcd1", "etcd2"},
		AuthorizedKeys: []string{"key1", "key2"},
	}
	expMinion := db.Minion{
		Self:           true,
		Spec:           "spec",
		Role:           db.Master,
		PrivateIP:      "priv",
		Provider:       "provider",
		Size:           "size",
		Region:         "region",
		AuthorizedKeys: "key1\nkey2",
	}
	_, err := s.SetMinionConfig(nil, &cfg)
	assert.NoError(t, err)
	checkMinionEquals(t, s.Conn, expMinion)
	checkEtcdEquals(t, s.Conn, db.Etcd{
		EtcdIPs: []string{"etcd1", "etcd2"},
	})

	// Update a field.
	cfg.Spec = "new"
	expMinion.Spec = "new"
	cfg.EtcdMembers = []string{"etcd3"}
	_, err = s.SetMinionConfig(nil, &cfg)
	assert.NoError(t, err)
	checkMinionEquals(t, s.Conn, expMinion)
	checkEtcdEquals(t, s.Conn, db.Etcd{
		EtcdIPs: []string{"etcd3"},
	})
}

func checkMinionEquals(t *testing.T, conn db.Conn, exp db.Minion) {
	timeout := time.After(1 * time.Second)
	var actual db.Minion
	for {
		actual, _ = conn.MinionSelf()
		actual.ID = 0
		if reflect.DeepEqual(exp, actual) {
			return
		}
		select {
		case <-timeout:
			t.Errorf("Expected minion to be %v, but got %v\n", exp, actual)
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func checkEtcdEquals(t *testing.T, conn db.Conn, exp db.Etcd) {
	timeout := time.After(1 * time.Second)
	var actual db.Etcd
	for {
		conn.Txn(db.AllTables...).Run(func(view db.Database) error {
			actual, _ = view.GetEtcd()
			return nil
		})
		actual.ID = 0
		if reflect.DeepEqual(exp, actual) {
			return
		}
		select {
		case <-timeout:
			t.Errorf("Expected etcd row to be %v, but got %v\n", exp, actual)
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestGetMinionConfig(t *testing.T) {
	t.Parallel()
	s := server{db.New()}

	// Should set Role to None if no config.
	cfg, err := s.GetMinionConfig(nil, &pb.Request{})
	assert.NoError(t, err)
	assert.Equal(t, pb.MinionConfig{Role: pb.MinionConfig_NONE}, *cfg)

	// Should only return config for "self".
	s.Conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMinion()
		m.Self = false
		m.Spec = "spec"
		m.Role = db.Master
		m.PrivateIP = "priv"
		m.Provider = "provider"
		m.Size = "size"
		m.Region = "region"
		m.AuthorizedKeys = "key1\nkey2"
		view.Commit(m)
		return nil
	})
	cfg, err = s.GetMinionConfig(nil, &pb.Request{})
	assert.NoError(t, err)
	assert.Equal(t, pb.MinionConfig{Role: pb.MinionConfig_NONE}, *cfg)

	// Test returning a full config.
	s.Conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.SelectFromMinion(nil)[0]
		m.Self = true
		view.Commit(m)

		etcd := view.InsertEtcd()
		etcd.EtcdIPs = []string{"etcd1", "etcd2"}
		view.Commit(etcd)
		return nil
	})
	cfg, err = s.GetMinionConfig(nil, &pb.Request{})
	assert.NoError(t, err)
	assert.Equal(t, pb.MinionConfig{
		Role:           pb.MinionConfig_MASTER,
		PrivateIP:      "priv",
		Spec:           "spec",
		Provider:       "provider",
		Size:           "size",
		Region:         "region",
		EtcdMembers:    []string{"etcd1", "etcd2"},
		AuthorizedKeys: []string{"key1", "key2"},
	}, *cfg)
}
