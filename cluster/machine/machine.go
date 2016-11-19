package machine

import (
	"fmt"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/stitch"
)

// Description describes a VM type offered by a cloud provider.
type Description struct {
	Size   string
	Price  float64
	RAM    float64
	CPU    int
	Disk   string
	Region string
}

// Machine represents an instance of a machine booted by a Provider.
type Machine struct {
	ID         string
	PublicIP   string
	PrivateIP  string
	FloatingIP string
	Reserved   bool
	Size       string
	DiskSize   int
	SSHKeys    []string
	Provider   db.Provider
	Region     string
	Role       db.Role
}

// ChooseSize returns an acceptable machine size for the given provider that fits the
// provided ram, cpu, and price constraints.
func ChooseSize(provider db.Provider, ram, cpu stitch.Range, maxPrice float64) string {
	switch provider {
	case db.Amazon:
		return chooseBestSize(amazonDescriptions, ram, cpu, maxPrice)
	case db.DigitalOcean:
		return chooseBestSize(digitalOceanDescriptions, ram, cpu, maxPrice)
	case db.Google:
		return chooseBestSize(googleDescriptions, ram, cpu, maxPrice)
	case db.Vagrant:
		return vagrantSize(ram, cpu)
	default:
		panic(fmt.Sprintf("Unknown Cloud Provider: %s", provider))
	}
}

// GroupByRegion groups machines by region.
func GroupByRegion(machines []Machine) map[string][]Machine {
	grouped := make(map[string][]Machine)
	for _, machine := range machines {
		region := machine.Region
		if _, ok := grouped[region]; !ok {
			grouped[region] = []Machine{}
		}
		grouped[region] = append(grouped[region], machine)
	}

	return grouped
}

func chooseBestSize(descriptions []Description, ram, cpu stitch.Range,
	maxPrice float64) string {
	var best Description
	for _, d := range descriptions {
		if ram.Accepts(d.RAM) &&
			cpu.Accepts(float64(d.CPU)) &&
			(best.Size == "" || d.Price < best.Price) {
			best = d
		}
	}
	if maxPrice == 0 || best.Price <= maxPrice {
		return best.Size
	}
	return ""
}

func vagrantSize(ramRange, cpuRange stitch.Range) string {
	ram := ramRange.Min
	if ram < 1 {
		ram = 1
	}

	cpu := cpuRange.Min
	if cpu < 1 {
		cpu = 1
	}
	return fmt.Sprintf("%g,%g", ram, cpu)
}
