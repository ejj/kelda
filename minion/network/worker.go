package network

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/ovsdb"
	"github.com/NetSys/quilt/stitch"

	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// This represents a rule in the iptables
type ipRule struct {
	cmd   string
	chain string
	opts  string // Must be sorted - see makeIPRule
}

type ipRuleSlice []ipRule

// OFRule is an OpenFlow rule for Open vSwitch's OpenFlow table.
type OFRule struct {
	table   string
	match   string
	actions string
}

// OFRuleSlice is an alias for []OFRule to allow for joins
type OFRuleSlice []OFRule

func runWorker(conn db.Conn) {
	minion, err := conn.MinionSelf()
	if err != nil || !minion.SupervisorInit || minion.Role != db.Worker {
		return
	}

	odb, err := ovsdb.Open()
	if err != nil {
		log.Warning("Failed to connect to ovsdb-server: %s", err)
		return
	}
	defer odb.Close()

	// XXX: By doing all the work within a transaction, we (kind of) guarantee that
	// containers won't be removed while we're in the process of setting them up.
	// Not ideal, but for now it's good enough.
	conn.Txn(db.ConnectionTable, db.ContainerTable, db.LabelTable,
		db.MinionTable).Run(func(view db.Database) error {

		if !checkSupervisorInit(view) {
			// Avoid a race condition where minion.SupervisorInit changed to
			// false since we checked above.
			return nil
		}

		containers := view.SelectFromContainer(func(c db.Container) bool {
			return c.DockerID != "" && c.IP != "" && c.Mac != "" &&
				c.Pid != 0
		})
		labels := view.SelectFromLabel(func(l db.Label) bool {
			return l.IP != ""
		})
		connections := view.SelectFromConnection(nil)

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			updateNAT(containers, connections)
			wg.Done()
		}()

		wg.Add(1)
		go func() {
			updateContainerIPs(containers, labels)
			wg.Done()
		}()

		// Ports must be updated before OpenFlow so they must be done in the same
		// go routine.
		updatePorts(odb, containers)
		updateOpenFlow(odb, containers, labels, connections)

		wg.Wait()
		return nil
	})
}

func updateNAT(containers []db.Container, connections []db.Connection) {
	publicInterface, err := getPublicInterface()
	if err != nil {
		log.WithError(err).Error("Failed to get public interface")
		return
	}

	targetRules := generateTargetNatRules(publicInterface, containers, connections)
	currRules, err := generateCurrentNatRules()
	if err != nil {
		log.WithError(err).Error("failed to get NAT rules")
		return
	}

	_, rulesToDel, rulesToAdd := join.HashJoin(currRules, targetRules, nil, nil)

	for _, rule := range rulesToDel {
		if err := deleteNatRule(rule.(ipRule)); err != nil {
			log.WithError(err).Error("failed to delete ip rule")
			continue
		}
	}

	for _, rule := range rulesToAdd {
		if err := addNatRule(rule.(ipRule)); err != nil {
			log.WithError(err).Error("failed to add ip rule")
			continue
		}
	}
}

func generateCurrentNatRules() (ipRuleSlice, error) {
	stdout, _, err := shVerbose("iptables -t nat -S")
	if err != nil {
		return nil, fmt.Errorf("failed to get IP tables: %s", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	var rules ipRuleSlice

	for scanner.Scan() {
		line := scanner.Text()

		rule, err := makeIPRule(line)
		if err != nil {
			return nil, fmt.Errorf("failed to get current IP rules: %s", err)
		}
		rules = append(rules, rule)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error while getting IP tables: %s", err)
	}
	return rules, nil
}

func generateTargetNatRules(publicInterface string, containers []db.Container,
	connections []db.Connection) ipRuleSlice {
	strRules := []string{
		"-P PREROUTING ACCEPT",
		"-P INPUT ACCEPT",
		"-P OUTPUT ACCEPT",
		"-P POSTROUTING ACCEPT",
		fmt.Sprintf("-A POSTROUTING -s 10.0.0.0/8 -o %s -j MASQUERADE",
			publicInterface),
	}

	protocols := []string{"tcp", "udp"}
	// Map each container IP to all ports on which it can receive packets
	// from the public internet.
	portsFromWeb := make(map[string]map[int]struct{})

	for _, dbc := range containers {
		for _, conn := range connections {

			if conn.From != stitch.PublicInternetLabel {
				continue
			}

			for _, l := range dbc.Labels {

				if conn.To != l {
					continue
				}

				if _, ok := portsFromWeb[dbc.IP]; !ok {
					portsFromWeb[dbc.IP] = make(map[int]struct{})
				}

				portsFromWeb[dbc.IP][conn.MinPort] = struct{}{}
			}
		}
	}

	// Map the container's port to the same port of the host.
	for ip, ports := range portsFromWeb {
		for port := range ports {
			for _, protocol := range protocols {
				strRules = append(strRules, fmt.Sprintf(
					"-A PREROUTING -i %[1]s "+
						"-p %[2]s -m %[2]s --dport %[3]d -j "+
						"DNAT --to-destination %[4]s:%[3]d",
					publicInterface, protocol, port, ip))
			}
		}
	}

	var rules ipRuleSlice
	for _, r := range strRules {
		rule, err := makeIPRule(r)
		if err != nil {
			panic("malformed target NAT rule")
		}
		rules = append(rules, rule)
	}
	return rules
}

// There certain exceptions, as certain ports will never be deleted.
func updatePorts(odb ovsdb.Client, containers []db.Container) {
	// An Open vSwitch patch port is referred to as a "port".
	targetPorts := generateTargetPorts(containers)
	currentPorts, err := odb.ListInterfaces()
	if err != nil {
		log.WithError(err).Error("failed to generate current openflow ports")
		return
	}

	key := func(val interface{}) interface{} {
		return struct {
			name, bridge string
		}{
			name:   val.(ovsdb.Interface).Name,
			bridge: val.(ovsdb.Interface).Bridge,
		}
	}

	pairs, lefts, rights := join.HashJoin(ovsdb.InterfaceSlice(currentPorts),
		targetPorts, key, key)

	for _, l := range lefts {
		if l.(ovsdb.Interface).Type == ovsdb.InterfaceTypeGeneve ||
			l.(ovsdb.Interface).Type == ovsdb.InterfaceTypeSTT ||
			l.(ovsdb.Interface).Type == ovsdb.InterfaceTypeInternal {
			// The "bridge" port and overlay port should never be deleted.
			continue
		}
		if err := odb.DeleteInterface(l.(ovsdb.Interface)); err != nil {
			log.WithError(err).Error("failed to delete openflow port")
			continue
		}
	}
	for _, r := range rights {
		iface := r.(ovsdb.Interface)
		if err := odb.CreateInterface(iface.Bridge, iface.Name); err != nil {
			log.WithError(err).Warning("error creating openflow port")
		}
		if err := odb.ModifyInterface(iface); err != nil {
			log.WithError(err).Error("error changing openflow port")
		}
	}
	for _, p := range pairs {
		l := p.L.(ovsdb.Interface)
		r := p.R.(ovsdb.Interface)
		if l.Type == r.Type && l.Peer == r.Peer &&
			l.AttachedMAC == r.AttachedMAC && l.IfaceID == r.IfaceID {
			continue
		}

		if err := odb.ModifyInterface(r); err != nil {
			log.WithError(err).Error("failed to modify openflow port")
			continue
		}
	}
}

func generateTargetPorts(containers []db.Container) ovsdb.InterfaceSlice {
	var configs ovsdb.InterfaceSlice
	for _, dbc := range containers {
		vethOut := ipdef.IFName(dbc.EndpointID)
		peerBr, peerQuilt := patchPorts(dbc.DockerID)
		configs = append(configs, ovsdb.Interface{
			Name:   vethOut,
			Bridge: quiltBridge,
		})
		configs = append(configs, ovsdb.Interface{
			Name:   peerQuilt,
			Bridge: quiltBridge,
			Type:   ovsdb.InterfaceTypePatch,
			Peer:   peerBr,
		})
		configs = append(configs, ovsdb.Interface{
			Name:        peerBr,
			Bridge:      ovnBridge,
			Type:        ovsdb.InterfaceTypePatch,
			Peer:        peerQuilt,
			AttachedMAC: dbc.Mac,
			IfaceID:     dbc.IP,
		})
	}
	return configs
}

func updateContainerIPs(containers []db.Container, labels []db.Label) {
	// XXX: Once we move to official OVN load balancer, containers will no longer
	// have multiple IP addresses and this function will become redundant.
	// Therefore, since the code is temporary, we're refraining from unit testing it
	// for now.  If that changes, tests should be added.
	labelIP := make(map[string]string)
	for _, l := range labels {
		labelIP[l.Label] = l.IP
	}

	// It's not safe for this go routine to be assigned to a different thread while
	// we're messing with network namespaces so we lock it to its current one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for _, c := range containers {
		updateContainerIP(c, labelIP)
	}
}

func updateContainerIP(dbc db.Container, labelIPs map[string]string) {
	ns, err := netns.GetFromPath(fmt.Sprintf("/hostproc/%d/ns/net", dbc.Pid))
	if err != nil {
		log.WithError(err).Warn("Failed to get container namespace")
		return
	}
	defer ns.Close()

	nlh, err := netlink.NewHandleAt(ns)
	if err != nil {
		log.WithError(err).Warn("Failed to get netlink handle")
		return
	}
	defer nlh.Delete()

	eth0, err := nlh.LinkByName("eth0")
	if err != nil {
		log.WithError(err).Warn("Failed to find eth0")
		return
	}

	eth0IPs, err := nlh.AddrList(eth0, 0)
	if err != nil {
		log.WithError(err).Warn("Failed to list eth0 IP addresses")
		return
	}

	var currIPs []string
	for _, ip := range eth0IPs {
		currIPs = append(currIPs, ip.String())
	}

	targetIPs := generateTargetIPs(dbc, labelIPs)

	_, ipToDel, ipToAdd := join.HashJoin(join.StringSlice(currIPs),
		join.StringSlice(targetIPs), nil, nil)

	dbcIP := net.ParseIP(dbc.IP)
	for _, ip := range ipToDel {
		addr, err := netlink.ParseAddr(ip.(string))
		if err != nil {
			panic(fmt.Sprintf("Not Reached: %v", err))
		}

		// Don't delete the container IP.
		if addr.IP.Equal(dbcIP) {
			continue
		}

		nlh.AddrDel(eth0, addr)
	}

	for _, ip := range ipToAdd {
		addr, err := netlink.ParseAddr(ip.(string))
		if err != nil {
			panic(fmt.Sprintf("Not Reached: %v", err))
		}
		nlh.AddrAdd(eth0, addr)
	}
}

func generateTargetIPs(dbc db.Container, labelIPs map[string]string) []string {
	ipSet := map[string]struct{}{}
	for _, l := range dbc.Labels {
		if ip := labelIPs[l]; ip != "" {
			ipSet[ip] = struct{}{}
		}
	}

	var targetIPs []string
	for ip := range ipSet {
		targetIPs = append(targetIPs, ip+"/8")
	}

	return targetIPs
}

// Sets up the OpenFlow tables to get packets from containers into the OVN controlled
// bridge.  The Openflow tables are organized as follows.
//
//     - Table 0 will check for packets destined to an ip address of a label with MAC
//     0A:00:00:00:00:00 (obtained by OVN faking out arp) and use the OF multipath action
//     to balance load packets across n links where n is the number of containers
//     implementing the label.  This result is stored in NXM_NX_REG0. This is done using
//     a symmetric l3/4 hash, so transport connections should remain intact.
//
//     -Table 1 reads NXM_NX_REG0 and changes the destination mac address to one of the
//     MACs of the containers that implement the label
//
// XXX: The multipath action doesn't perform well.  We should migrate away from it
// choosing datapath recirculation instead.
func updateOpenFlow(odb ovsdb.Client, containers []db.Container,
	labels []db.Label, connections []db.Connection) {

	targetOF, err := generateTargetOpenFlow(odb, containers, labels, connections)
	if err != nil {
		log.WithError(err).Error("failed to get target OpenFlow flows")
		return
	}
	currentOF, err := generateCurrentOpenFlow()
	if err != nil {
		log.WithError(err).Error("failed to get current OpenFlow flows")
		return
	}

	_, flowsToDel, flowsToAdd := join.HashJoin(currentOF, targetOF, nil, nil)

	if err := addOrDelFlows(flowsToDel, false); err != nil {
		log.WithError(err).Error("error deleting OpenFlow flow")
	}

	if err := addOrDelFlows(flowsToAdd, true); err != nil {
		log.WithError(err).Error("error adding OpenFlow flow")
	}
}

func generateCurrentOpenFlow() (OFRuleSlice, error) {
	stdout, err := exec.Command("ovs-ofctl", "dump-flows", quiltBridge).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list OpenFlow flows: %s", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	var flows OFRuleSlice

	// The first line isn't a flow, so skip it.
	scanner.Scan()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		flow, err := makeOFRule(line)

		if err != nil {
			return nil, fmt.Errorf("failed to make OpenFlow rule: %s", err)
		}

		flows = append(flows, flow)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error while getting OpenFlow flows: %s",
			err)
	}

	return flows, nil
}

// The target flows must be in the same format as the output from ovs-ofctl
// dump-flows. To achieve this, we have some rather ugly hacks that handle
// a few special cases.
func generateTargetOpenFlow(odb ovsdb.Client, containers []db.Container,
	labels []db.Label, connections []db.Connection) (OFRuleSlice, error) {

	ifaces, err := odb.ListInterfaces()
	if err != nil {
		return nil, err
	}

	ifaceMap := make(map[string]int)
	for _, iface := range ifaces {
		if iface.OFPort != nil {
			ifaceMap[iface.Name] = *iface.OFPort
		}
	}

	gwMac := ipdef.GatewayMac

	var rules []string
	for _, dbc := range containers {
		vethOut := ipdef.IFName(dbc.EndpointID)
		_, peerQuilt := patchPorts(dbc.DockerID)
		dbcMac := dbc.Mac

		ofQuilt, ok := ifaceMap[peerQuilt]
		if !ok {
			continue
		}

		ofVeth, ok := ifaceMap[vethOut]
		if !ok {
			continue
		}

		if ofQuilt < 0 || ofVeth < 0 {
			continue
		}

		rules = append(rules, []string{
			fmt.Sprintf("table=0 priority=5000,in_port=%d "+
				"actions=output:%d", ofQuilt, ofVeth),
			fmt.Sprintf("table=2 priority=5000,in_port=%d "+
				"actions=output:%d", ofVeth, ofQuilt),
			fmt.Sprintf("table=0 priority=0,in_port=%d "+
				"actions=output:%d", ofVeth, ofQuilt),

			// Always send DNS packets to and from the default gateway.
			fmt.Sprintf("table=0 priority=5001,"+
				"in_port=%d,udp,dl_src=%s,dl_dst=%s,nw_dst=%s,tp_dst=53"+
				" actions=LOCAL", ofVeth, dbcMac, gwMac,
				ipdef.GatewayIP),
			fmt.Sprintf("table=0 priority=5001,in_port=LOCAL,udp,dl_src=%s,"+
				"dl_dst=%s,nw_src=%s,tp_src=53 actions=output:%d",
				gwMac, dbcMac, ipdef.GatewayIP, ofVeth),
		}...)

		protocols := []string{"tcp", "udp"}

		portsToWeb := make(map[int]struct{})
		portsFromWeb := make(map[int]struct{})
		for _, l := range dbc.Labels {
			for _, conn := range connections {
				if conn.From == l &&
					conn.To == stitch.PublicInternetLabel {
					portsToWeb[conn.MinPort] = struct{}{}
				} else if conn.From ==
					stitch.PublicInternetLabel && conn.To == l {
					portsFromWeb[conn.MinPort] = struct{}{}
				}
			}
		}

		// LOCAL is the default quilt-int port created with the bridge.
		egressRule := fmt.Sprintf("table=0 priority=5000,in_port=%d,", ofVeth) +
			"%s,%s," + fmt.Sprintf("dl_dst=%s actions=LOCAL", gwMac)
		ingressRule := "table=0 priority=5000,in_port=LOCAL,%s,%s," +
			fmt.Sprintf("dl_dst=%s actions=output:%d", dbcMac, ofVeth)
		for port := range portsFromWeb {
			for _, protocol := range protocols {
				egressPort := fmt.Sprintf("tp_src=%d", port)
				rules = append(rules, fmt.Sprintf(egressRule, protocol,
					egressPort))

				ingressPort := fmt.Sprintf("tp_dst=%d", port)
				rules = append(rules, fmt.Sprintf(ingressRule, protocol,
					ingressPort))
			}
		}

		for port := range portsToWeb {
			for _, protocol := range protocols {
				egressPort := fmt.Sprintf("tp_dst=%d", port)
				rules = append(rules, fmt.Sprintf(egressRule, protocol,
					egressPort))

				ingressPort := fmt.Sprintf("tp_src=%d", port)
				rules = append(rules, fmt.Sprintf(ingressRule, protocol,
					ingressPort))
			}
		}

		if len(portsToWeb) > 0 || len(portsFromWeb) > 0 {
			// Allow ICMP
			rules = append(rules,
				fmt.Sprintf("table=0 priority=5000,icmp,in_port=%d,"+
					"dl_dst=%s actions=LOCAL", ofVeth, gwMac))
			rules = append(rules,
				fmt.Sprintf(
					"table=0 priority=5000,icmp,in_port=LOCAL,"+
						"dl_dst=%s actions=output:%d",
					dbcMac, ofVeth))
		}

		if len(portsFromWeb) > 0 {
			// Allow default gateway to ARP for containers
			rules = append(rules, fmt.Sprintf(
				"table=0 priority=%d,arp,in_port=LOCAL,"+
					"dl_dst=ff:ff:ff:ff:ff:ff actions=output:%d",
				4500, ofVeth))
		}

		rules = append(rules, fmt.Sprintf(
			"table=0 priority=4500,arp,in_port=%d "+
				"actions=output:%d,LOCAL", ofVeth, ofQuilt))
		rules = append(rules, fmt.Sprintf(
			"table=0 priority=4500,arp,in_port=LOCAL,"+
				"dl_dst=%s actions=output:%d", dbcMac, ofVeth))
	}

	LabelMacs := make(map[string]map[string]struct{})
	for _, dbc := range containers {
		for _, l := range dbc.Labels {
			if _, ok := LabelMacs[l]; !ok {
				LabelMacs[l] = make(map[string]struct{})
			}
			LabelMacs[l][dbc.Mac] = struct{}{}
		}
	}

	for _, label := range labels {
		if !label.MultiHost {
			continue
		}

		macs := LabelMacs[label.Label]
		if len(macs) == 0 {
			continue
		}

		ip := label.IP
		n := len(macs)
		lg2n := int(math.Ceil(math.Log2(float64(n))))

		nxmRange := fmt.Sprintf("0..%d", lg2n)
		if lg2n == 0 {
			// dump-flows collapses 0..0 to just 0.
			nxmRange = "0"
		}
		mpa := fmt.Sprintf("multipath(symmetric_l3l4,0,modulo_n,%d,0,"+
			"NXM_NX_REG0[%s])", n, nxmRange)

		rules = append(rules, fmt.Sprintf(
			"table=0 priority=%d,dl_dst=%s,ip,nw_dst=%s "+
				"actions=%s,resubmit(,1)",
			4000, labelMac, ip, mpa))

		// We need the order to make diffing consistent.
		macList := make([]string, 0, n)
		for mac := range macs {
			macList = append(macList, mac)
		}
		sort.Strings(macList)

		i := 0
		regPrefix := ""
		for _, mac := range macList {
			if i > 0 {
				// dump-flows puts a 0x prefix for all register values
				// except for 0.
				regPrefix = "0x"
			}
			reg0 := fmt.Sprintf("%s%x", regPrefix, i)

			rules = append(rules, fmt.Sprintf(
				"table=1 priority=5000,ip,nw_dst=%s,"+
					"reg0=%s actions=mod_dl_dst:%s,resubmit(,2)",
				ip, reg0, mac))
			i++
		}
	}

	var targetRules OFRuleSlice
	for _, r := range rules {
		rule, err := makeOFRule(r)
		if err != nil {
			return nil, fmt.Errorf("failed to make OpenFlow rule: %s", err)
		}
		targetRules = append(targetRules, rule)
	}

	return targetRules, nil
}

func patchPorts(id string) (br, quilt string) {
	return ipdef.IFName("br_" + id), ipdef.IFName("q_" + id)
}

// Returns (Stdout, Stderr, error)
//
// It's critical that the error returned here is the exact error
// from "os/exec" commands
var shVerbose = func(format string, args ...interface{}) (
	stdout, stderr []byte, err error) {
	command := fmt.Sprintf(format, args...)
	cmdArgs := strings.Split(command, " ")
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, nil, err
	}

	return outBuf.Bytes(), errBuf.Bytes(), nil
}

// makeIPRule takes an ip rule as formatted in the output of `iptables -S`,
// and returns the corresponding ipRule. The output options will be in the same
// order as output by `iptables -S`.
func makeIPRule(inputRule string) (ipRule, error) {
	cmdRE := regexp.MustCompile("(-[A-Z]+)\\s+([A-Z]+)")
	cmdMatch := cmdRE.FindSubmatch([]byte(inputRule))
	if len(cmdMatch) < 3 {
		return ipRule{}, fmt.Errorf("missing iptables command")
	}

	var opts string
	optsRE := regexp.MustCompile("-(?:[A-Z]+\\s+)+[A-Z]+\\s+(.*)")
	optsMatch := optsRE.FindSubmatch([]byte(inputRule))

	if len(optsMatch) > 2 {
		return ipRule{}, fmt.Errorf("malformed iptables options")
	}

	if len(optsMatch) == 2 {
		opts = strings.TrimSpace(string(optsMatch[1]))
	}

	rule := ipRule{
		cmd:   strings.TrimSpace(string(cmdMatch[1])),
		chain: strings.TrimSpace(string(cmdMatch[2])),
		opts:  opts,
	}
	return rule, nil
}

func deleteNatRule(rule ipRule) error {
	var command string
	args := fmt.Sprintf("%s %s", rule.chain, rule.opts)
	if rule.cmd == "-A" {
		command = fmt.Sprintf("iptables -t nat -D %s", args)
	} else if rule.cmd == "-N" {
		// Delete new chains.
		command = fmt.Sprintf("iptables -t nat -X %s", rule.chain)
	}

	stdout, _, err := shVerbose(command)
	if err != nil {
		return fmt.Errorf("failed to delete NAT rule %s: %s", command,
			string(stdout))
	}
	return nil
}

func addNatRule(rule ipRule) error {
	args := fmt.Sprintf("%s %s", rule.chain, rule.opts)
	cmd := fmt.Sprintf("iptables -t nat -A %s", args)
	_, _, err := shVerbose(cmd)

	if err != nil {
		return fmt.Errorf("failed to add NAT rule %s: %s", cmd, err)
	}
	return nil
}

// getPublicInterface gets the interface with the default route.
func getPublicInterface() (string, error) {
	routes, err := routeList(nil, 0)
	if err != nil {
		return "", fmt.Errorf("route list: %s", err)
	}

	var defaultRoute *netlink.Route
	for _, r := range routes {
		if r.Dst == nil {
			defaultRoute = &r
			break
		}
	}

	if defaultRoute == nil {
		return "", errors.New("missing default route")
	}

	link, err := linkByIndex(defaultRoute.LinkIndex)
	if err != nil {
		return "", fmt.Errorf("default route missing interface: %s", err)
	}

	return link.Attrs().Name, err
}

func addOrDelFlows(flows []interface{}, add bool) error {
	if len(flows) == 0 {
		return nil
	}

	logAction := "additions"
	args := []string{"add-flows", quiltBridge, "-"}
	if !add {
		args = []string{"del-flows", "--strict", quiltBridge, "-"}
		logAction = "deletions"
	}
	cmd := exec.Command("ovs-ofctl", args...)

	log.Infof("%d OpenFlow %s", len(flows), logAction)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("error running ovs-ofctl: %s", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ovs-ofctl: %s", err)
	}

	for _, f := range flows {
		flow := f.(OFRule)
		rule := fmt.Sprintf("%s,%s", flow.table, flow.match)
		if add {
			rule += fmt.Sprintf(",actions=%s", flow.actions)
		}
		stdin.Write([]byte(rule + "\n"))
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("error running ovs-ofctl: %s", err)
	}

	return nil
}

// makeOFRule constructs an OFRule with the given flow, actions and table.
// table must be of the format table=X, and both flow and action must be
// formatted as in the output from `ovs-ofctl dump-flows` - this includes
// case sensitivity. There must be no spaces in flow and actions.
// This function works for the flows we need at the moment, but it will likely
// have to be extended in the future.
func makeOFRule(flowEntry string) (OFRule, error) {
	tableRE := regexp.MustCompile("table=\\d+")
	if ok := tableRE.MatchString(flowEntry); !ok {
		return OFRule{}, fmt.Errorf("malformed OpenFlow table: %s", flowEntry)
	}
	table := tableRE.FindString(flowEntry)

	fields := strings.Split(flowEntry, " ")
	match := fields[len(fields)-2]
	actions := fields[len(fields)-1]

	actRE := regexp.MustCompile("actions=(\\S+)")
	actMatches := actRE.FindStringSubmatch(actions)
	if len(actMatches) != 2 {
		return OFRule{}, fmt.Errorf("bad OF action format: %s", actions)
	}
	actions = actMatches[1]

	// An action of the format function(args...)
	funcRE := regexp.MustCompile("\\w+\\([^\\)]+\\)")
	funcMatches := funcRE.FindAllString(actions, -1)

	// Remove all functions, so we can split actions on commas
	noFuncs := funcRE.Split(actions, -1)

	var argMatches []string
	for _, a := range noFuncs {
		// XXX: If there are commas between two functions in actions,
		// there might be lone commas in noFuncs. We should ideally
		// get rid of these commas with the regex above.
		act := strings.Trim(string(a), ",")
		if act == "" {
			continue
		}

		splitAct := strings.Split(act, ",")
		argMatches = append(argMatches, splitAct...)
	}

	allMatches := append(argMatches, funcMatches...)
	sort.Strings(allMatches)

	splitMatch := strings.Split(match, ",")
	sort.Strings(splitMatch)

	newRule := OFRule{
		table:   table,
		match:   strings.Join(splitMatch, ","),
		actions: strings.Join(allMatches, ","),
	}
	return newRule, nil
}

func (iprs ipRuleSlice) Get(ii int) interface{} {
	return iprs[ii]
}

func (iprs ipRuleSlice) Len() int {
	return len(iprs)
}

// Get returns the value contained at the given index
func (ofrs OFRuleSlice) Get(ii int) interface{} {
	return ofrs[ii]
}

// Len returns the number of items in the slice
func (ofrs OFRuleSlice) Len() int {
	return len(ofrs)
}

var routeList = netlink.RouteList
var linkByIndex = netlink.LinkByIndex
