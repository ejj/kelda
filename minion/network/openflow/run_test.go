package openflow

import (
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/assert"
)

func TestOpenflowContainers(t *testing.T) {
	containers := openflowContainers(
		[]db.Container{{IP: "1.2.3.4", Hostname: "foo"}},
		[]db.Connection{{
			From:    []string{"public"},
			To:      []string{"foo"},
			MinPort: 80,
			MaxPort: 80,
		}, {
			From:    []string{"foo"},
			To:      []string{"public"},
			MinPort: 22,
			MaxPort: 22,
		}, {
			From:    []string{"foo"},
			To:      []string{"public"},
			MinPort: 1,
			MaxPort: 1000}})

	assert.Equal(t, []Container{{
		Veth:    "1.2.3.4",
		Patch:   "q_1.2.3.4",
		Mac:     "02:00:01:02:03:04",
		IP:      "1.2.3.4",
		ToPub:   map[int]struct{}{22: {}},
		FromPub: map[int]struct{}{80: {}},
	}}, containers)

}
