package db

//The Role within the cluster each machine assumes.
import (
	"errors"

	"github.com/kelda/kelda/minion/pb"
)

// The Role a machine may take on within the cluster.
type Role string

const (
	// None is for workers who haven't been assigned a role yet.
	None Role = ""

	// Worker minions run application containers.
	Worker = "Worker"

	// Master containers provide services for the Worker containers.
	Master = "Master"
)

// RoleToPB converts db.Role to a protobuf role.
func RoleToPB(r Role) pb.MinionConfig_Role {
	switch r {
	case None:
		return pb.MinionConfig_NONE
	case Worker:
		return pb.MinionConfig_WORKER
	case Master:
		return pb.MinionConfig_MASTER
	default:
		panic("Not Reached")
	}
}

// PBToRole converts a protobuf role to a db.Role.
func PBToRole(p pb.MinionConfig_Role) Role {
	switch p {
	case pb.MinionConfig_NONE:
		return None
	case pb.MinionConfig_WORKER:
		return Worker
	case pb.MinionConfig_MASTER:
		return Master
	default:
		panic("Not Reached")
	}
}

// ProviderName describes one of the supported cloud providers. The strings
// enumerated below must exactly match the name provided by users' JavaScript.
type ProviderName string

const (
	// Amazon implements Amazon EC2.
	Amazon ProviderName = "Amazon"

	// Google implements Google Cloud Engine.
	Google ProviderName = "Google"

	// DigitalOcean implements Digital Ocean Droplets.
	DigitalOcean ProviderName = "DigitalOcean"

	// Vagrant implements local virtual machines.
	Vagrant ProviderName = "Vagrant"
)

// AllProviders lists all of the providers that Quilt supports.
var AllProviders = []ProviderName{
	Amazon,
	Google,
	DigitalOcean,
	Vagrant,
}

// ParseRole returns the Role represented by the string 'role', or an error.
func ParseRole(role string) (Role, error) {
	switch role {
	case "Master":
		return Master, nil
	case "Worker":
		return Worker, nil
	case "":
		return None, nil
	default:
		return None, errors.New("unknown role")
	}
}
