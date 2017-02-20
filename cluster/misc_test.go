package cluster

import (
	"testing"

	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
)

func TestDefaultRegion(t *testing.T) {
	exp := "foo"
	m := db.Machine{Provider: "Amazon", Region: exp}
	m = DefaultRegion(m)
	if m.Region != exp {
		t.Errorf("expected %s, found %s", exp, m.Region)
	}

	m.Region = ""
	m = DefaultRegion(m)
	exp = "us-west-1"
	if m.Region != exp {
		t.Errorf("expected %s, found %s", exp, m.Region)
	}

	m.Region = ""
	m.Provider = "Google"
	exp = "us-east1-b"
	m = DefaultRegion(m)
	if m.Region != exp {
		t.Errorf("expected %s, found %s", exp, m.Region)
	}

	m.Region = ""
	m.Provider = "Vagrant"
	exp = ""
	m = DefaultRegion(m)
	if m.Region != exp {
		t.Errorf("expected %s, found %s", exp, m.Region)
	}

	m.Region = ""
	m.Provider = "Panic"
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic")
		}
	}()

	m = DefaultRegion(m)
}

func TestNewProviderFailure(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("provider.New did not panic on invalid provider")
		}
	}()
	newProviderImpl("FakeAmazon", testRegion, "namespace")
}

func TestGroupBy(t *testing.T) {
	machines := []machine.Machine{
		{Provider: db.Google}, {Provider: db.Amazon}, {Provider: db.Google},
		{Provider: db.Google},
	}
	grouped := groupBy(machines)
	m := grouped[instance{db.Amazon, ""}]
	if len(m) != 1 || m[0].Provider != machines[1].Provider {
		t.Errorf("wrong Amazon machines: %v", m)
	}
	m = grouped[instance{db.Google, ""}]
	if len(m) != 3 {
		t.Errorf("wrong Google machines: %v", m)
	} else {
		for _, machine := range m {
			if machine.Provider != db.Google {
				t.Errorf("machine provider is not Google: %v", machine)
			}
		}
	}
	m = grouped[instance{db.Vagrant, ""}]
	if len(m) != 0 {
		t.Errorf("unexpected Vagrant machines: %v", m)
	}
}
