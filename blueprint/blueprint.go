package blueprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kelda/kelda/util"
)

// A Blueprint is an abstract representation of the policy language.
type Blueprint struct {
	Containers    []Container    `json:",omitempty"`
	LoadBalancers []LoadBalancer `json:",omitempty"`
	Connections   []Connection   `json:",omitempty"`
	Placements    []Placement    `json:",omitempty"`
	Machines      []Machine      `json:",omitempty"`

	AdminACL  []string `json:",omitempty"`
	Namespace string   `json:",omitempty"`
}

// A Placement constraint guides on what type of machine a container can be
// scheduled.
type Placement struct {
	TargetContainer string `json:",omitempty"`

	Exclusive bool `json:",omitempty"`

	// Machine Constraints
	Provider   string `json:",omitempty"`
	Size       string `json:",omitempty"`
	Region     string `json:",omitempty"`
	FloatingIP string `json:",omitempty"`
}

// An Image represents a Docker image that can be run. If the Dockerfile is non-empty,
// the image should be built and hosted by Kelda.
type Image struct {
	Name       string `json:",omitempty"`
	Dockerfile string `json:",omitempty"`
}

// A Container may be instantiated in the blueprint and queried by users.
type Container struct {
	ID                string                    `json:",omitempty"`
	Image             Image                     `json:",omitempty"`
	Command           []string                  `json:",omitempty"`
	Env               map[string]ContainerValue `json:",omitempty"`
	FilepathToContent map[string]ContainerValue `json:",omitempty"`
	Hostname          string                    `json:",omitempty"`
}

// ContainerValue is a wrapper for the possible values that can be used in
// the container Env and FilepathToContent maps. The only permissible types
// are Secret and string.
type ContainerValue struct {
	Value interface{}
}

// Secret represents the name of a secret whose value is stored in Vault. The
// caller is expected to query Vault to resolve the secret value.
type Secret struct {
	NameOfSecret string
}

// A LoadBalancer represents a load balanced group of containers.
type LoadBalancer struct {
	Name      string   `json:",omitempty"`
	Hostnames []string `json:",omitempty"`
}

// A Connection allows any container whose hostname appears in `From` to speak with any
// container whose hostname appears in `To` using ports in the range [MinPort, MaxPort]
type Connection struct {
	From    []string `json:",omitempty"`
	To      []string `json:",omitempty"`
	MinPort int      `json:",omitempty"`
	MaxPort int      `json:",omitempty"`
}

// A ConnectionSlice allows for slices of Collections to be used in joins
type ConnectionSlice []Connection

// A Machine specifies the type of VM that should be booted.
type Machine struct {
	Provider    string   `json:",omitempty"`
	Role        string   `json:",omitempty"`
	Size        string   `json:",omitempty"`
	DiskSize    int      `json:",omitempty"`
	Region      string   `json:",omitempty"`
	SSHKeys     []string `json:",omitempty"`
	FloatingIP  string   `json:",omitempty"`
	Preemptible bool     `json:",omitempty"`
}

// PublicInternetLabel is a magic label that allows connections to or from the public
// network.
const PublicInternetLabel = "public"

var lookPath = exec.LookPath

// FromFile gets a Blueprint handle from a file on disk.
func FromFile(filename string) (Blueprint, error) {
	return FromFileWithArgs(filename, nil)
}

// FromFileWithArgs gets a Blueprint handle from a file on disk, passing the
// given arguments to the node process.
func FromFileWithArgs(filename string, cmdLineArgs []string) (Blueprint, error) {
	nodeBinary, err := util.GetNodeBinary()
	if err != nil {
		return Blueprint{}, err
	}

	outFile, err := ioutil.TempFile("", "kelda-out")
	if err != nil {
		return Blueprint{}, fmt.Errorf(
			"failed to create deployment file: %s", err)
	}
	defer outFile.Close()
	defer os.Remove(outFile.Name())

	absPath, err := filepath.Abs(filename)
	if err != nil {
		return Blueprint{}, fmt.Errorf(
			"failed to get path to blueprint: %s", err)
	}
	args := []string{"-e", fmt.Sprintf(
		`// Normally, when users run a node process, process.argv[1] is the
                // absolute path to the node script. However, when running node with
                // the -e flag, the script name naturally isn't part of process.argv.
                // To emulate the "normal" process.argv, which most users will be
                // familiar with, we insert their blueprint path at index 1.
                process.argv.splice(1, 0, "%s");

                require("%s");
                require('fs').writeFileSync("%s",
                  JSON.stringify(global.getInfrastructureKeldaRepr())
            );`, absPath, filename, outFile.Name())}
	args = append(args, cmdLineArgs...)

	cmd := exec.Command(nodeBinary, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return Blueprint{}, err
	}

	depl, err := ioutil.ReadAll(outFile)
	if err != nil {
		return Blueprint{}, fmt.Errorf("failed to read deployment file: %s", err)
	}
	return FromJSON(string(depl))
}

// FromJSON gets a Blueprint handle from the deployment representation.
func FromJSON(jsonStr string) (bp Blueprint, err error) {
	err = json.Unmarshal([]byte(jsonStr), &bp)
	if err != nil {
		err = fmt.Errorf("unable to parse blueprint: %s", err)
	}
	return bp, err
}

// String returns the Blueprint in its deployment representation.
func (bp Blueprint) String() string {
	jsonBytes, err := json.Marshal(bp)
	if err != nil {
		panic(err)
	}
	return string(jsonBytes)
}

// NewSecret returns a ContainerValue representing a secret.
func NewSecret(name string) ContainerValue {
	return ContainerValue{Secret{name}}
}

// NewString returns a ContainerValue representing a string.
func NewString(str string) ContainerValue {
	return ContainerValue{str}
}

// DivideContainerValues divides a map of ContainerValues into two maps -- one
// of string values, and one of secrets.
func DivideContainerValues(vals map[string]ContainerValue) (
	rawStrings, secrets map[string]string) {

	rawStrings = map[string]string{}
	secrets = map[string]string{}
	for name, valIntf := range vals {
		switch val := valIntf.Value.(type) {
		case string:
			rawStrings[name] = val
		case Secret:
			secrets[name] = val.NameOfSecret
		default:
			panic("unreached")
		}
	}
	return
}

// String returns a human-readable representation of the ContainerValue. This
// makes the database logs easier to read.
func (cv ContainerValue) String() string {
	switch v := cv.Value.(type) {
	case string:
		return v
	case Secret:
		return "Secret: " + v.NameOfSecret
	default:
		return fmt.Sprintf("%+v", v)
	}
}

// UnmarshalJSON implements the unmarshal interface for converting JSON into Go
// structs. A custom unmarshaller is necessary because ContainerValue contains
// an interface, so the default Go unmarshaller cannot infer what type the
// JSON should be unmarshalled to.
func (cv *ContainerValue) UnmarshalJSON(jsonBytes []byte) error {
	var tryString string
	stringErr := json.Unmarshal(jsonBytes, &tryString)
	if stringErr == nil {
		cv.Value = tryString
		return nil
	}

	trySecret, secretErr := unmarshalAsSecret(jsonBytes)
	if secretErr == nil {
		cv.Value = trySecret
		return nil
	}

	return fmt.Errorf("not a Secret (%s) or string (%s)", secretErr, stringErr)
}

func unmarshalAsSecret(jsonBytes []byte) (Secret, error) {
	secret := Secret{}
	if err := json.Unmarshal(jsonBytes, &secret); err != nil {
		return secret, err
	}

	if secret.NameOfSecret == "" {
		return secret, errors.New("missing required field: NameOfSecret")
	}
	return secret, nil
}

// MarshalJSON implements the Go interface for automatically serializing
// structs into JSON.
func (cv ContainerValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(cv.Value)
}

// Get returns the value contained at the given index
func (cs ConnectionSlice) Get(ii int) interface{} {
	return cs[ii]
}

// Len returns the number of items in the slice
func (cs ConnectionSlice) Len() int {
	return len(cs)
}
