package db

import (
	"fmt"
	"sort"
	"strings"
)

// Machine represents a physical or virtual machine operated by a cloud provider on
// which containers may be run.
type Machine struct {
	ID int //Database ID

	Role        Role
	Provider    ProviderName
	Region      string
	Size        string
	DiskSize    int
	SSHKeys     []string `rowStringer:"omit"`
	FloatingIP  string
	Preemptible bool

	/* Populated by the cloud provider. */
	CloudID   string //Cloud Provider ID
	PublicIP  string
	PrivateIP string

	/* Populated by the cluster. */
	Status string
}

const (
	// Stopping represents a machine that is being stopped by a cloud provider.
	Stopping = "stopping"

	// Booting represents that the machine is being booted by a cloud provider.
	Booting = "booting"

	// Connecting represents that the machine is booted, but we have not yet
	// successfully connected.
	Connecting = "connecting"

	// Reconnecting represents that we connected at one point, but are
	// currently disconnected.
	Reconnecting = "reconnecting"

	// Connected represents that we are currently connected to the machine's
	// minion.
	Connected = "connected"
)

// InsertMachine creates a new Machine and inserts it into 'db'.
func (db Database) InsertMachine() Machine {
	result := Machine{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromMachine gets all machines in the database that satisfy the 'check'.
func (db Database) SelectFromMachine(check func(Machine) bool) []Machine {
	var result []Machine
	for _, row := range db.selectRows(MachineTable) {
		if check == nil || check(row.(Machine)) {
			result = append(result, row.(Machine))
		}
	}
	return result
}

// SelectFromMachine gets all machines in the database that satisfy 'check'.
func (cn Conn) SelectFromMachine(check func(Machine) bool) []Machine {
	var machines []Machine
	cn.Txn(MachineTable).Run(func(view Database) error {
		machines = view.SelectFromMachine(check)
		return nil
	})
	return machines
}

func (m Machine) getID() int {
	return m.ID
}

func (m Machine) String() string {
	var tags []string

	if m.Role != "" {
		tags = append(tags, string(m.Role))
	}

	machineAttrs := []string{string(m.Provider), m.Region, m.Size}
	if m.Preemptible {
		machineAttrs = append(machineAttrs, "preemptible")
	}
	tags = append(tags, strings.Join(machineAttrs, " "))

	if m.CloudID != "" {
		tags = append(tags, m.CloudID)
	}

	if m.PublicIP != "" {
		tags = append(tags, "PublicIP="+m.PublicIP)
	}

	if m.PrivateIP != "" {
		tags = append(tags, "PrivateIP="+m.PrivateIP)
	}

	if m.FloatingIP != "" {
		tags = append(tags, fmt.Sprintf("FloatingIP=%s", m.FloatingIP))
	}

	if m.DiskSize != 0 {
		tags = append(tags, fmt.Sprintf("Disk=%dGB", m.DiskSize))
	}

	if m.Status != "" {
		tags = append(tags, m.Status)
	}

	return fmt.Sprintf("Machine-%d{%s}", m.ID, strings.Join(tags, ", "))
}

func (m Machine) less(arg row) bool {
	l, r := m, arg.(Machine)
	switch {
	case l.Role != r.Role:
		return l.Role == Master || r.Role == ""
	case l.CloudID != r.CloudID:
		return l.CloudID > r.CloudID // Prefer non-zero IDs.
	default:
		return l.ID < r.ID
	}
}

// SortMachines returns a slice of machines sorted according to the default database
// sort order.
func SortMachines(machines []Machine) []Machine {
	rows := make([]row, 0, len(machines))
	for _, m := range machines {
		rows = append(rows, m)
	}

	sort.Sort(rowSlice(rows))

	machines = make([]Machine, 0, len(machines))
	for _, r := range rows {
		machines = append(machines, r.(Machine))
	}

	return machines
}

// MachineSlice is an alias for []Machine to allow for joins
type MachineSlice []Machine

// Get returns the value contained at the given index
func (ms MachineSlice) Get(ii int) interface{} {
	return ms[ii]
}

// Len returns the number of items in the slice
func (ms MachineSlice) Len() int {
	return len(ms)
}
