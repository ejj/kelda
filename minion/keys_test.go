package minion

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"
)

type keyTest struct {
	dbKeys, keyFile, expKeyFile string
}

func TestSyncKeys(t *testing.T) {
	tests := []keyTest{
		{
			dbKeys:     "key1\nkey2",
			expKeyFile: "key1\nkey2",
		},
		{
			dbKeys:     "key1\nkey2",
			keyFile:    "key1",
			expKeyFile: "key1\nkey2",
		},
		{
			dbKeys:     "key1\nkey2",
			keyFile:    "key1\nkey2",
			expKeyFile: "key1\nkey2",
		},
		{
			keyFile:    "key1\nkey2",
			expKeyFile: "",
		},
	}
	for _, test := range tests {
		util.AppFs = afero.NewMemMapFs()
		if test.keyFile != "" {
			err := util.WriteFile(
				authorizedKeysFile, []byte(test.keyFile), 0644)
			assert.NoError(t, err)
		}

		conn := db.New()
		conn.Txn(db.AllTables...).Run(func(view db.Database) error {
			m := view.InsertMinion()
			m.Self = true
			m.AuthorizedKeys = test.dbKeys
			view.Commit(m)
			return nil
		})

		err := runOnce(conn)
		assert.NoError(t, err)

		actual, err := util.ReadFile(authorizedKeysFile)
		assert.NoError(t, err)
		assert.Equal(t, test.expKeyFile, actual)
	}
}

func TestSyncKeysError(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	conn := db.New()
	err := runOnce(conn)
	assert.EqualError(t, err, "no self minion")

	fs := afero.NewMemMapFs()
	util.AppFs = afero.NewReadOnlyFs(fs)
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMinion()
		m.Self = true
		m.AuthorizedKeys = "keys"
		view.Commit(m)
		return nil
	})
	err = runOnce(conn)
	assert.EqualError(t, err, "open /home/quilt/.ssh/authorized_keys: "+
		"file does not exist")

	fs.Create(authorizedKeysFile)
	err = runOnce(conn)
	assert.EqualError(t, err, "operation not permitted")
}
