package cluster

import (
	"fmt"

	"github.com/quilt/quilt/cluster/amazon"
	"github.com/quilt/quilt/cluster/google"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
)

// DefaultRegion populates `m.Region` for the provided db.Machine if one isn't
// specified. This is intended to allow users to omit the cloud provider region when
// they don't particularly care where a system is placed.
func DefaultRegion(m db.Machine) db.Machine {
	if m.Region != "" {
		return m
	}

	switch m.Provider {
	case db.Amazon:
		m.Region = amazon.DefaultRegion
	case db.Google:
		m.Region = google.DefaultRegion
	case db.Vagrant:
	default:
		panic(fmt.Sprintf("Unknown Cloud Provider: %s", m.Provider))
	}

	return m
}

// ChooseSize returns an acceptable machine size for the given provider that fits the
// provided ram, cpu, and price constraints.
var ChooseSize = machine.ChooseSize
