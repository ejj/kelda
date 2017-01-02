package network

import (
	"reflect"
	"testing"

	"github.com/NetSys/quilt/db"
)

func TestMakeIPRule(t *testing.T) {
	inp := "-A INPUT -p tcp -i eth0 -m multiport --dports 465,110,995 -j ACCEPT"
	rule, _ := makeIPRule(inp)
	expCmd := "-A"
	expChain := "INPUT"
	expOpts := "-p tcp -i eth0 -m multiport --dports 465,110,995 -j ACCEPT"

	if rule.cmd != expCmd {
		t.Errorf("Bad ipRule command.\nExpected:\n%s\n\nGot:\n%s\n",
			expCmd, rule.cmd)
	}

	if rule.chain != expChain {
		t.Errorf("Bad ipRule chain.\nExpected:\n%s\n\nGot:\n%s\n",
			expChain, rule.chain)
	}

	if rule.opts != expOpts {
		t.Errorf("Bad ipRule options.\nExpected:\n%s\n\nGot:\n%s\n",
			expOpts, rule.opts)
	}

	inp = "-A POSTROUTING -s 10.0.3.0/24 ! -d 10.0.3.0/24 -j MASQUERADE"
	rule, _ = makeIPRule(inp)
	expCmd = "-A"
	expChain = "POSTROUTING"
	expOpts = "-s 10.0.3.0/24 ! -d 10.0.3.0/24 -j MASQUERADE"

	if rule.cmd != expCmd {
		t.Errorf("Bad ipRule command.\nExpected:\n%s\n\nGot:\n%s\n",
			expCmd, rule.cmd)
	}

	if rule.chain != expChain {
		t.Errorf("Bad ipRule chain.\nExpected:\n%s\n\nGot:\n%s\n",
			expChain, rule.chain)
	}

	if rule.opts != expOpts {
		t.Errorf("Bad ipRule options.\nExpected:\n%s\n\nGot:\n%s\n",
			expOpts, rule.opts)
	}

	inp = "-A PREROUTING -i eth0 -p tcp --dport 80 -j DNAT " +
		"--to-destination 10.31.0.23:80"
	rule, _ = makeIPRule(inp)
	expCmd = "-A"
	expChain = "PREROUTING"
	expOpts = "-i eth0 -p tcp --dport 80 -j DNAT --to-destination 10.31.0.23:80"

	if rule.cmd != expCmd {
		t.Errorf("Bad ipRule command.\nExpected:\n%s\n\nGot:\n%s\n",
			expCmd, rule.cmd)
	}

	if rule.chain != expChain {
		t.Errorf("Bad ipRule chain.\nExpected:\n%s\n\nGot:\n%s\n",
			expChain, rule.chain)
	}

	if rule.opts != expOpts {
		t.Errorf("Bad ipRule options.\nExpected:\n%s\n\nGot:\n%s\n",
			expOpts, rule.opts)
	}
}

func TestGenerateCurrentNatRules(t *testing.T) {
	oldShVerbose := shVerbose
	defer func() { shVerbose = oldShVerbose }()
	shVerbose = func(format string, args ...interface{}) (
		stdout, stderr []byte, err error) {
		return []byte(rules()), nil, nil
	}

	actual, _ := generateCurrentNatRules()
	exp := ipRuleSlice{
		{
			cmd:   "-P",
			chain: "POSTROUTING",
			opts:  "ACCEPT",
		},
		{
			cmd:   "-N",
			chain: "DOCKER",
		},
		{
			cmd:   "-A",
			chain: "POSTROUTING",
			opts:  "-s 11.0.0.0/8,10.0.0.0/8 -o eth0 -j MASQUERADE",
		},
		{
			cmd:   "-A",
			chain: "POSTROUTING",
			opts:  "-s 10.0.3.0/24 ! -d 10.0.3.0/24 -j MASQUERADE",
		},
	}

	if !(reflect.DeepEqual(actual, exp)) {
		t.Errorf("Generated wrong routes.\nExpected:\n%+v\n\nGot:\n%+v\n",
			exp, actual)
	}
}

func defaultLabelsConnections() (map[string]db.Label, map[string][]string) {

	labels := map[string]db.Label{
		"red": {
			IP:           "10.0.0.1",
			ContainerIPs: []string{"1.2.2.2", "1.3.3.3", "1.4.4.4"},
		},
		"blue": {
			IP:           "10.0.0.2",
			ContainerIPs: []string{"1.3.3.3", "1.4.4.4"},
		},
		"green": {
			IP:           "10.0.0.3",
			ContainerIPs: []string{"1.1.1.1"},
		},
	}

	connections := map[string][]string{
		"red":  {"blue", "green"},
		"blue": {"red"},
	}

	return labels, connections
}

func localhosts() string {
	return `
127.0.0.1       localhost
::1             localhost ip6-localhost ip6-loopback
fe00::0         ip6-localnet
ff00::0         ip6-mcastprefix
ff02::1         ip6-allnodes
ff02::2         ip6-allrouters
`
}

func routes() string {
	return `default via 10.0.2.2 dev eth0
	10.0.2.0/24 dev eth0  proto kernel  scope link  src 10.0.2.15
	192.168.162.0/24 dev eth1  proto kernel  scope link  src 192.168.162.162`
}

func rules() string {
	return `-P POSTROUTING ACCEPT
-N DOCKER
-A POSTROUTING -s 11.0.0.0/8,10.0.0.0/8 -o eth0 -j MASQUERADE
-A POSTROUTING -s 10.0.3.0/24 ! -d 10.0.3.0/24 -j MASQUERADE`
}
