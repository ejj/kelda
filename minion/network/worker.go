package network

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/ovsdb"
	"github.com/NetSys/quilt/stitch"

	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

// This represents a rule in the iptables
type ipRule struct {
	cmd   string
	chain string
	opts  string // Must be sorted - see makeIPRule
}

type ipRuleSlice []ipRule

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
	defer odb.Disconnect()

	// XXX: By doing all the work within a transaction, we (kind of) guarantee that
	// containers won't be removed while we're in the process of setting them up.
	// Not ideal, but for now it's good enough.
	conn.Txn(db.ConnectionTable, db.ContainerTable,
		db.MinionTable).Run(func(view db.Database) error {

		if !checkSupervisorInit(view) {
			// Avoid a race condition where minion.SupervisorInit changed to
			// false since we checked above.
			return nil
		}

		containers := view.SelectFromContainer(func(c db.Container) bool {
			return c.DockerID != "" && c.IP != ""
		})
		connections := view.SelectFromConnection(nil)

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			updateNAT(containers, connections)
			wg.Done()
		}()

		updateOpenFlow(odb, containers)

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

func updateOpenFlow(odb ovsdb.Client, containers []db.Container) {
	ifaceMap, err := odb.OpenFlowPorts()
	if err != nil {
		log.WithError(err).Error("Failed to get OpenFlow ports")
		return
	}

	err = ofctlReplaceFlows(generateOpenFlow(generateOFPorts(ifaceMap, containers)))
	if err != nil {
		log.WithError(err).Error("error replacing OpenFlow")
		return
	}
}

func generateOFPorts(ifaceMap map[string]int, dbcs []db.Container) []ofPort {
	var ofcs []ofPort
	for _, dbc := range dbcs {
		vethOut := ipdef.IFName(dbc.EndpointID)
		_, peerQuilt := ipdef.PatchPorts(dbc.EndpointID)

		ofVeth, ok := ifaceMap[vethOut]
		if !ok {
			continue
		}

		ofQuilt, ok := ifaceMap[peerQuilt]
		if !ok {
			continue
		}

		ofcs = append(ofcs, ofPort{
			PatchPort: ofQuilt,
			VethPort:  ofVeth,
			Mac:       ipdef.IPStrToMac(dbc.IP),
		})
	}
	return ofcs
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

func ofctlReplaceFlows(flows []string) error {
	cmd := exec.Command("ovs-ofctl", "-O", "OpenFlow13", "replace-flows",
		ipdef.QuiltBridge, "-")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	for _, f := range flows {
		stdin.Write([]byte(f + "\n"))
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func (iprs ipRuleSlice) Get(ii int) interface{} {
	return iprs[ii]
}

func (iprs ipRuleSlice) Len() int {
	return len(iprs)
}

var routeList = netlink.RouteList
var linkByIndex = netlink.LinkByIndex
