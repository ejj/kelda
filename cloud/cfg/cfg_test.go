package cfg

import (
	"testing"

	"github.com/kelda/kelda/db"

	log "github.com/sirupsen/logrus"
)

func TestCloudConfig(t *testing.T) {
	cfgTemplate = "({{.KeldaImage}}) ({{.SSHKeys}}) " +
		"({{.MinionOpts}}) ({{.LogLevel}}) ({{.KeldaHome}})"

	log.SetLevel(log.InfoLevel)
	image = "keldaio/test:master"
	res := Ubuntu(db.Machine{
		SSHKeys: []string{"a", "b"},
		Role:    db.Master,
	}, "")
	exp := "(keldaio/test:master) (a\nb) (--role \"Master\") (info)" +
		" (/home/kelda/.kelda)"
	if res != exp {
		t.Errorf("res: %s\nexp: %s", res, exp)
	}

	log.SetLevel(log.DebugLevel)
	image = "keldaio/test:1.2.3"
	res = Ubuntu(db.Machine{
		SSHKeys: []string{"a", "b"},
		Role:    db.Worker,
	}, "ib")
	exp = "(keldaio/test:1.2.3) (a\nb) (--role \"Worker\"" +
		" --inbound-pub-intf \"ib\") (debug)" +
		" (/home/kelda/.kelda)"
	if res != exp {
		t.Errorf("res: %s\nexp: %s", res, exp)
	}
}
