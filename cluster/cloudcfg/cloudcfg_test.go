package cloudcfg

import (
	"testing"

	"github.com/quilt/quilt/db"
)

func TestCloudConfig(t *testing.T) {
	cfgTemplate = "({{.QuiltImage}}) ({{.SSHKeys}}) ({{.UbuntuVersion}}) " +
		"({{.Role}})"

	ver = "master"
	res := Ubuntu([]string{"a", "b"}, db.Master)
	exp := "(quilt/quilt:master) (a\nb) (xenial) (Master)"
	if res != exp {
		t.Errorf("res: %s\nexp: %s", res, exp)
	}

	ver = "1.2.3"
	res = Ubuntu([]string{"a", "b"}, db.Worker)
	exp = "(quilt/quilt:1.2.3) (a\nb) (xenial) (Worker)"
	if res != exp {
		t.Errorf("res: %s\nexp: %s", res, exp)
	}

}
