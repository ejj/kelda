package network

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/ipdef"

	log "github.com/Sirupsen/logrus"
	"github.com/miekg/dns"
)

const dnsTTL = 60 // Seconds

type dnsTable struct {
	server dns.Server

	recordLock sync.Mutex
	records    map[string]net.IP
}

var table *dnsTable

func runDNS(conn db.Conn) {
	for range conn.Trigger(db.LabelTable, db.MinionTable).C {
		runDNSOnce(conn)
	}
}

func runDNSOnce(conn db.Conn) {
	self, err := conn.MinionSelf()
	if err != nil {
		log.WithError(err).Debug("Failed to get self")
		return
	}

	if self.Role != db.Worker {
		if table == nil {
			return
		}

		err := table.server.Shutdown()
		if err != nil {
			log.WithError(err).Error("Failed to shut down DNS server")
		}
		table = nil
		return
	}

	if !self.SupervisorInit {
		return
	}

	table = updateTable(table, conn.SelectFromLabel(nil))
}

func updateTable(table *dnsTable, labels []db.Label) *dnsTable {
	records := labelsToDNS(labels)
	if table != nil {
		table.recordLock.Lock()
		table.records = records
		table.recordLock.Unlock()
		return table
	}
	table = makeTable(records)

	// There could be multiple messages depending on how listenAndServe is
	// implemented.  We don't want anyone to block, so we make a bit of a buffer.
	errChan := make(chan error, 8)
	table.server.NotifyStartedFunc = func() { errChan <- nil }
	go func() { errChan <- listenAndServe(table) }()

	if err := <-errChan; err != nil {
		log.WithError(err).Error("Failed to start DNS server")
		return nil
	}

	log.Info("Started DNS Server")
	return table
}

func (table *dnsTable) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	defer w.Close()

	log.Debug("DNS Request: ", req)

	resp := table.genResponse(req)
	if resp == nil {
		return
	}
	log.Debug("DNS Response: ", resp)

	if err := w.WriteMsg(resp); err != nil {
		log.WithError(err).Error("Failed to send DNS response")
	}
}

func (table *dnsTable) genResponse(req *dns.Msg) *dns.Msg {
	resp := &dns.Msg{}
	if len(req.Question) != 1 {
		return resp.SetRcode(req, dns.RcodeNotImplemented)
	}
	q := req.Question[0]
	if q.Qclass != dns.ClassINET || q.Qtype != dns.TypeA {
		return resp.SetRcode(req, dns.RcodeNotImplemented)
	}

	ips := table.lookupA(q.Name)
	if len(ips) == 0 {
		// Even though the client asked for a hostname within `.q` that we know
		// nothing about, it's possible we'll learn about it in the future.  For
		// now, we'll just not respond, the client will time out, and try again
		// later.  Hopefully by then we have a response for them -- or if not,
		// eventually they'll give up.
		//
		// XXX: The above logic is correct for things in the .q domain name, but
		// we're also doing the same thing for failures to resolve external
		// hosts.  This isn't entirely correct, it would be much better to return
		// whatever upstream gave us in case of a failure.
		return nil
	}

	resp.SetReply(req)
	for _, ip := range ips {
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    dnsTTL,
			},
			A: ip,
		})
	}
	return resp
}

func (table *dnsTable) lookupA(name string) []net.IP {
	if strings.HasSuffix(name, ".q.") {
		table.recordLock.Lock()
		ip := table.records[name]
		table.recordLock.Unlock()
		if ip == nil {
			return nil
		}
		return []net.IP{ip}
	}

	ipStrs, err := lookupHost(strings.TrimRight(name, "."))
	if err != nil {
		log.WithError(err).Debug("Failed to lookup external record: ", name)
		return nil
	}

	var ips []net.IP
	for _, ipStr := range ipStrs {
		if ip := net.ParseIP(ipStr); ip != nil && ip.To4() != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

func makeTable(records map[string]net.IP) *dnsTable {
	tbl := &dnsTable{
		records: records,
		server: dns.Server{
			Addr: fmt.Sprintf("%s:53", ipdef.GatewayIP),
			Net:  "udp",
		},
	}
	tbl.server.Handler = tbl
	return tbl
}

func labelsToDNS(labels []db.Label) map[string]net.IP {
	records := map[string]net.IP{}
	for _, label := range labels {
		if ip := net.ParseIP(label.IP); ip != nil {
			records[label.Label+".q."] = ip
		}

		for i, ipStr := range label.ContainerIPs {
			if ip := net.ParseIP(ipStr); ip != nil {
				records[fmt.Sprintf("%d.%s.q.", i+1, label.Label)] = ip
			}
		}
	}
	return records
}

var listenAndServe = func(table *dnsTable) error {
	return table.server.ListenAndServe()
}

var lookupHost = net.LookupHost
