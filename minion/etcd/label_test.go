package etcd

import (
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/stretchr/testify/assert"
)

func TestRunLabelOnce(t *testing.T) {
	t.Parallel()

	store := newTestMock()
	conn := db.New()

	err := runLabelOnce(conn, store)
	assert.Error(t, err)

	err = store.Set(labelPath, "", 0)
	assert.NoError(t, err)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.InsertEtcd()
		etcd.Leader = true
		view.Commit(etcd)

		label := view.InsertLabel()
		label.Label = "Robot"
		label.IP = "1.2.3.5"
		view.Commit(label)
		return nil
	})

	err = runLabelOnce(conn, store)
	assert.NoError(t, err)

	str, err := store.Get(labelPath)
	assert.NoError(t, err)

	expStr := `[
    {
        "Label": "Robot",
        "IP": "1.2.3.5",
        "ContainerIPs": null
    }
]`
	assert.Equal(t, expStr, str)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.SelectFromEtcd(nil)[0]
		etcd.Leader = false
		view.Commit(etcd)

		label := view.SelectFromLabel(nil)[0]
		label.IP = "1.2.3.4"
		view.Commit(label)
		return nil
	})

	err = runLabelOnce(conn, store)
	assert.NoError(t, err)

	explabel := db.Label{
		Label: "Robot",
		IP:    "1.2.3.5",
	}
	labels := conn.SelectFromLabel(nil)
	assert.Len(t, labels, 1)
	labels[0].ID = 0
	assert.Equal(t, explabel, labels[0])

	err = runLabelOnce(conn, store)
	assert.NoError(t, err)

	labels = conn.SelectFromLabel(nil)
	assert.Len(t, labels, 1)
	labels[0].ID = 0
	assert.Equal(t, explabel, labels[0])
}
