package network

import (
	"net"
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

func TestUpdateTable(t *testing.T) {
	t.Parallel()

	listenAndServe = func(table *dnsTable) error { return assert.AnError }
	assert.Nil(t, updateTable(nil, nil))

	listenAndServe = func(table *dnsTable) error {
		table.server.NotifyStartedFunc()
		return nil
	}

	table := updateTable(nil, []db.Label{{Label: "foo", IP: "1.2.3.4"}})
	assert.NotNil(t, table)
	assert.Equal(t, map[string]net.IP{"foo.q.": net.IPv4(1, 2, 3, 4)}, table.records)

	newTable := updateTable(table, []db.Label{{Label: "foo", IP: "5.6.7.8"}})
	assert.NotNil(t, newTable)
	assert.True(t, table == newTable) // Pointer Equality.
	assert.Equal(t, map[string]net.IP{"foo.q.": net.IPv4(5, 6, 7, 8)},
		newTable.records)
}

func TestGenResponse(t *testing.T) {
	t.Parallel()

	table := makeTable(map[string]net.IP{
		"a.q.": net.IPv4(1, 2, 3, 4),
	})

	req := &dns.Msg{}
	req.SetQuestion("foo.", dns.TypeAAAA)
	resp := table.genResponse(req)
	assert.Equal(t, req.Id, resp.Id)
	assert.Equal(t, resp.Rcode, dns.RcodeNotImplemented)

	req.Question = nil
	resp = table.genResponse(req)
	assert.Equal(t, req.Id, resp.Id)
	assert.Equal(t, dns.RcodeNotImplemented, resp.Rcode)

	req.SetQuestion("bad.q.", dns.TypeA)
	resp = table.genResponse(req)
	assert.Nil(t, resp)

	req.SetQuestion("a.q.", dns.TypeA)
	resp = table.genResponse(req)
	exp := *req
	exp.Response = true
	exp.Rcode = dns.RcodeSuccess
	exp.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   "a.q.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    dnsTTL,
		},
		A: net.IPv4(1, 2, 3, 4),
	}}
	assert.Equal(t, &exp, resp)

}

func TestLookupA(t *testing.T) {
	t.Parallel()

	table := makeTable(map[string]net.IP{
		"a.q.": net.IPv4(1, 2, 3, 4),
	})

	assert.Empty(t, table.lookupA("bad.q."))
	assert.Equal(t, []net.IP{net.IPv4(1, 2, 3, 4)}, table.lookupA("a.q."))

	lookupHost = func(string) ([]string, error) { return nil, assert.AnError }
	assert.Empty(t, table.lookupA("quilt.io."))

	lookupHost = func(string) ([]string, error) { return []string{"bad"}, nil }
	assert.Empty(t, table.lookupA("quilt.io."))

	lookupHost = func(string) ([]string, error) {
		return []string{"2601:644:380:cde:fc06:2533:adf9:2891"}, nil
	}
	assert.Empty(t, table.lookupA("quilt.io."))

	lookupHost = func(string) ([]string, error) {
		return []string{"1.2.3.4", "5.6.7.8"}, nil
	}
	assert.Equal(t, []net.IP{net.IPv4(1, 2, 3, 4), net.IPv4(5, 6, 7, 8)},
		table.lookupA("quilt.io."))
}

func TestMakeTable(t *testing.T) {
	t.Parallel()

	records := map[string]net.IP{"a": net.IPv4(1, 2, 3, 4)}
	tbl := makeTable(records)
	assert.Equal(t, tbl.records, records)
	assert.Equal(t, tbl.server.Addr, "10.0.0.1:53")
	assert.Equal(t, tbl.server.Net, "udp")
}

func TestLabelsToDNS(t *testing.T) {
	t.Parallel()

	res := labelsToDNS([]db.Label{{
		Label: "l1",
	}, {
		Label:        "l2",
		IP:           "badIP",
		ContainerIPs: []string{"another", "bad", "IP"},
	}, {
		Label: "l3",
		IP:    "1.2.3.4",
	}, {
		Label:        "l4",
		IP:           "5.6.7.8",
		ContainerIPs: []string{"1.1.1.1", "2.2.2.2"},
	}})
	exp := map[string]net.IP{
		"l3.q.":   net.IPv4(1, 2, 3, 4),
		"l4.q.":   net.IPv4(5, 6, 7, 8),
		"1.l4.q.": net.IPv4(1, 1, 1, 1),
		"2.l4.q.": net.IPv4(2, 2, 2, 2),
	}
	assert.Equal(t, exp, res)
}
