package vagrant

import (
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/assert"
)

func TestSetACLs(t *testing.T) {
	prvdr := Provider{}
	assert.Nil(t, prvdr.SetACLs(nil))
}

func TestPreemptibleError(t *testing.T) {
	ids, err := Provider{}.Boot([]db.Machine{{Preemptible: true}})
	assert.EqualError(t, err, "vagrant does not support preemptible instances")
	assert.Nil(t, ids)
}
