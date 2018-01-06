package cfg

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/version"

	log "github.com/sirupsen/logrus"
)

// Allow mocking out for the unit tests.
var image = version.Image

// Ubuntu generates a cloud config file for the Ubuntu operating system with the
// corresponding `version`.
func Ubuntu(m db.Machine, inboundPublic string) string {
	t := template.Must(template.New("cloudConfig").Parse(cfgTemplate))

	var cloudConfigBytes bytes.Buffer
	err := t.Execute(&cloudConfigBytes, struct {
		KeldaImage string
		SSHKeys    string
		LogLevel   string
		MinionOpts string
		KeldaHome  string
	}{
		KeldaImage: image,
		SSHKeys:    strings.Join(m.SSHKeys, "\n"),
		LogLevel:   log.GetLevel().String(),
		MinionOpts: minionOptions(m.Role, inboundPublic),
		KeldaHome:  cliPath.MinionHome,
	})
	if err != nil {
		panic(err)
	}

	return cloudConfigBytes.String()
}

func minionOptions(role db.Role, inboundPublic string) string {
	options := fmt.Sprintf("--role %q", role)

	if inboundPublic != "" {
		options += fmt.Sprintf(" --inbound-pub-intf %q", inboundPublic)
	}
	return options
}
