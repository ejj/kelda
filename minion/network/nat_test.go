package network

//go:generate mockery -name=IPTables

import (
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/mocks"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/nl/nlmock"
)

func TestUpdateNATErrors(t *testing.T) {
	ipt := &mocks.IPTables{}
	anErr := errors.New("err")

	getDefaultRouteIntf = func() (string, error) {
		return "", anErr
	}
	assert.NotNil(t, updateNAT(ipt, nil, nil, "", ""))

	ipt = &mocks.IPTables{}
	ipt.On("AppendUnique", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(anErr)
	getDefaultRouteIntf = func() (string, error) {
		return "eth0", nil
	}
	assert.NotNil(t, updateNAT(ipt, nil, nil, "", ""))

	ipt = &mocks.IPTables{}
	ipt.On("AppendUnique", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil)
	ipt.On("List", mock.Anything, mock.Anything).Return(nil, anErr)
	assert.NotNil(t, updateNAT(ipt, nil, nil, "", ""))

	ipt = &mocks.IPTables{}
	ipt.On("AppendUnique", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything).Return(nil)
	ipt.On("List", "nat", "PREROUTING").Return(nil, nil)
	ipt.On("List", "nat", "POSTROUTING").Return(nil, anErr)
	assert.NotNil(t, updateNAT(ipt, nil, nil, "", ""))
}

func TestPreroutingRules(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			IP:       "8.8.8.8",
			Hostname: "red",
		},
		{
			IP:       "9.9.9.9",
			Hostname: "purple",
		},
	}

	connections := []db.Connection{
		{
			From:    []string{blueprint.PublicInternetLabel},
			To:      []string{"red"},
			MinPort: 80,
		},
		{
			From:    []string{blueprint.PublicInternetLabel},
			To:      []string{"purple"},
			MinPort: 81,
		},
		{
			From:    []string{"yellow"},
			To:      []string{blueprint.PublicInternetLabel},
			MinPort: 80,
		},
	}

	actual := preroutingRules("eth0", containers, connections)
	exp := []string{
		"-i eth0 -p tcp -m tcp --dport 80 -j DNAT --to-destination 8.8.8.8:80",
		"-i eth0 -p udp -m udp --dport 80 -j DNAT --to-destination 8.8.8.8:80",
		"-i eth0 -p tcp -m tcp --dport 81 -j DNAT --to-destination 9.9.9.9:81",
		"-i eth0 -p udp -m udp --dport 81 -j DNAT --to-destination 9.9.9.9:81",
	}
	assert.Equal(t, exp, actual)
}

func TestPostroutingRules(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			IP:       "8.8.8.8",
			Hostname: "red",
		},
		{
			IP:       "9.9.9.9",
			Hostname: "purple",
		},
	}

	connections := []db.Connection{
		{
			From:    []string{"red"},
			To:      []string{blueprint.PublicInternetLabel},
			MinPort: 80,
		},
		{
			From:    []string{"purple"},
			To:      []string{blueprint.PublicInternetLabel},
			MinPort: 81,
		},
	}

	exp := []string{
		"-s 8.8.8.8/32 -p tcp -m tcp --dport 80 -o eth0 -j MASQUERADE",
		"-s 8.8.8.8/32 -p udp -m udp --dport 80 -o eth0 -j MASQUERADE",
		"-s 9.9.9.9/32 -p tcp -m tcp --dport 81 -o eth0 -j MASQUERADE",
		"-s 9.9.9.9/32 -p udp -m udp --dport 81 -o eth0 -j MASQUERADE",
	}
	actual := postroutingRules("eth0", containers, connections)
	sort.Strings(actual)
	assert.Equal(t, exp, actual)
}

func TestGetRules(t *testing.T) {
	ipt := &mocks.IPTables{}
	ipt.On("List", "nat", "PREROUTING").Return([]string{
		"-A PREROUTING -j ACCEPT",
		"-P PREROUTING ACCEPT",
		"-A PREROUTING -i eth0 -j DNAT --to-destination 9.9.9.9:80",
	}, nil)
	actual, err := getRules(ipt, "nat", "PREROUTING")
	exp := []string{
		"-j ACCEPT",
		"-i eth0 -j DNAT --to-destination 9.9.9.9:80",
	}
	assert.NoError(t, err)
	assert.Equal(t, exp, actual)

	ipt = &mocks.IPTables{}
	ipt.On("List", "nat", "PREROUTING").Return([]string{
		"-A PREROUTING",
	}, nil)
	_, err = getRules(ipt, "nat", "PREROUTING")
	assert.NotNil(t, err)
}

func TestSyncChain(t *testing.T) {
	ipt := &mocks.IPTables{}
	ipt.On("List", "nat", "PREROUTING").Return([]string{
		"-A PREROUTING -i eth0 -j DNAT --to-destination 7.7.7.7:80",
		"-A PREROUTING -i eth0 -j DNAT --to-destination 8.8.8.8:80",
	}, nil)
	ipt.On("Delete", "nat", "PREROUTING",
		"-i", "eth0", "-j", "DNAT", "--to-destination", "7.7.7.7:80",
	).Return(nil)
	ipt.On("Append", "nat", "PREROUTING",
		"-i", "eth0", "-j", "DNAT", "--to-destination", "9.9.9.9:80",
	).Return(nil)
	err := syncChain(ipt, "nat", "PREROUTING", []string{
		"-i eth0 -j DNAT --to-destination 8.8.8.8:80",
		"-i eth0 -j DNAT --to-destination 9.9.9.9:80",
	})
	assert.NoError(t, err)
	ipt.AssertExpectations(t)

	anErr := errors.New("err")
	ipt = &mocks.IPTables{}
	ipt.On("List", mock.Anything, mock.Anything).Return(
		[]string{"-A PREROUTING deleteme"}, nil)
	ipt.On("Delete", mock.Anything, mock.Anything, mock.Anything).Return(anErr)
	err = syncChain(ipt, "nat", "PREROUTING", []string{})
	assert.NotNil(t, err)

	ipt = &mocks.IPTables{}
	ipt.On("List", mock.Anything, mock.Anything).Return(nil, nil)
	ipt.On("Append", mock.Anything, mock.Anything, mock.Anything).Return(anErr)
	err = syncChain(ipt, "nat", "PREROUTING", []string{"addme"})
	assert.NotNil(t, err)
}

func TestSyncChainOptionsOrder(t *testing.T) {
	ipt := &mocks.IPTables{}
	ipt.On("List", "nat", "POSTROUTING").Return([]string{
		"-A POSTROUTING -s 8.8.8.8/32 -p tcp --dport 80 -o eth0 -j MASQUERADE",
		"-A POSTROUTING -s 9.9.9.9/32 -p udp --dport 22 -o eth0 -j MASQUERADE",
	}, nil)
	err := syncChain(ipt, "nat", "POSTROUTING", []string{
		"-p tcp -s 8.8.8.8/32 -o eth0 --dport 80 -j MASQUERADE",
		"--dport 22 -s 9.9.9.9/32 -p udp -o eth0 -j MASQUERADE",
	})
	assert.NoError(t, err)
	ipt.AssertExpectations(t)
}

func TestRuleKey(t *testing.T) {
	assert.Equal(t,
		"[dport=80 j=MASQUERADE o=eth0 p=tcp s=8.8.8.8/32]",
		ruleKey("-s 8.8.8.8/32 -p tcp --dport 80 -o eth0 -j MASQUERADE"))
	assert.Equal(t,
		ruleKey("-p tcp -s 8.8.8.8/32 -o eth0 --dport 80 -j MASQUERADE"),
		ruleKey("-s 8.8.8.8/32 -p tcp --dport 80 -o eth0 -j MASQUERADE"))
	assert.NotEqual(t,
		ruleKey("-s 8.8.8.8/32 -p tcp --dport 81 -o eth0 -j MASQUERADE"),
		ruleKey("-s 8.8.8.8/32 -p tcp --dport 80 -o eth0 -j MASQUERADE"))

	assert.Equal(t,
		"[dport=80 i=eth0 j=DNAT --to-destination 8.8.8.8:80 m=tcp p=tcp]",
		ruleKey("-i eth0 -p tcp -m tcp --dport 80 "+
			"-j DNAT --to-destination 8.8.8.8:80"))
	assert.Equal(t,
		ruleKey("-p tcp  --dport 80 -i eth0 -m tcp "+
			"-j DNAT --to-destination 8.8.8.8:80"),
		ruleKey("-i eth0 -p tcp -m tcp --dport 80 "+
			"-j DNAT --to-destination 8.8.8.8:80"))

	assert.Nil(t, ruleKey("malformed"))
}

func TestGetDefaultRouteIntf(t *testing.T) {
	mockNetlink := new(nlmock.I)
	nl.N = mockNetlink
	mockNetlink.On("RouteList", mock.Anything).Once().Return(
		nil, errors.New("not implemented"))

	link := netlink.GenericLink{}
	link.LinkAttrs.Name = "link name"
	mockNetlink.On("LinkByIndex", 5).Return(&link, nil)
	mockNetlink.On("LinkByIndex", 2).Return(nil, errors.New("unknown"))

	res, err := getDefaultRouteIntfImpl()
	assert.Empty(t, res)
	assert.EqualError(t, err, "route list: not implemented")

	mockNetlink.On("RouteList", mock.Anything).Once().Return(nil, nil)
	res, err = getDefaultRouteIntfImpl()
	assert.Empty(t, res)
	assert.EqualError(t, err, "missing default route")

	mockNetlink.On("RouteList", mock.Anything).Once().Return(
		[]nl.Route{{Dst: &ipdef.KeldaSubnet}}, nil)
	res, err = getDefaultRouteIntfImpl()
	assert.Empty(t, res)
	assert.EqualError(t, err, "missing default route")

	mockNetlink.On("RouteList", mock.Anything).Once().Return(
		[]nl.Route{{LinkIndex: 2}}, nil)
	res, err = getDefaultRouteIntfImpl()
	assert.Empty(t, res)
	assert.EqualError(t, err, "default route missing interface: unknown")

	mockNetlink.On("RouteList", mock.Anything).Once().Return(
		[]nl.Route{{LinkIndex: 5}}, nil)
	res, err = getDefaultRouteIntfImpl()
	assert.Equal(t, "link name", res)
	assert.NoError(t, err)
}

func TestPickIntfs(t *testing.T) {
	getDefaultRouteIntf = func() (string, error) {
		return "default", nil
	}

	resInbound, resOutbound, err := pickIntfs("inbound", "outbound")
	assert.NoError(t, err)
	assert.Equal(t, "inbound", resInbound)
	assert.Equal(t, "outbound", resOutbound)

	resInbound, resOutbound, err = pickIntfs("inbound", "")
	assert.NoError(t, err)
	assert.Equal(t, "inbound", resInbound)
	assert.Equal(t, "default", resOutbound)
}
