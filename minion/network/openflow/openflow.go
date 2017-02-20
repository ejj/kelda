package openflow

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/quilt/quilt/minion/ipdef"
	"github.com/quilt/quilt/minion/ovsdb"
)

/* OpenFlow Psuedocode -- Please, for the love of God, keep this updated.

OpenFlow is extremely difficult to reason about -- especially when its buried in Go code.
This comment aims to make it a bit easier to maintain by describing abstractly what the
OpenFlow code does, without the distraction of the go code required to implement it.

Interpreting the Psuedocode
---------------------------
The OpenFlow code is divided into a series of tables.  Packets start at Table_0 and only
move to another table if explicitly instructed to by a `goto` statement.

Each table is composed of a series of if statements.  Packets match either one or zero of
these statements.  If they match zero they're dropped, if they match more than one then
the statement that appears first in the table is chosen.

Each if statement has one or more actions associated with it.  Packets matching the
statement execute those actions in order.  If one of those actions is a goto statement,
the packet is forwarded to the specified table and the process begins again.

Finally, note that some tables have loops which should be interpreted as duplicating the
inner if statements per loop element.

Registers
---------

The psuedocode currently uses three registers:

Reg0 -- Indicates what type of port the packet came from.  1 for a Veth.  2 for a patch
port. 0 if neither.

Reg1 -- Contains the OpenFlow port number of the veth, or zero if the packet came from
the gateway.

Reg2 -- Contains the OpenFlow port number of the patch port, or zero if the packet came
from the gateway.

Tables
------

// Table_0 initializes the registers and forwards to Table_1.
Table_0 { // Initial Table
	for each db.Container {
		if in_port=dbc.VethPort && dl_src=dbc.Mac {
			reg0 <- 1
			reg1 <- dbc.VethPort
			reg2 <- dbc.PatchPort
			goto Table_1
		}

		if in_port=dbc.PatchPort {
			reg0 <- 2
			reg1 <- dbc.VethPort
			reg2 <- dbc.PatchPort
			goto Table_1
		}
	}

	if in_port=LOCAL {
		goto Table_1
	}
}

// Table_1 handles special cases for broadcast packets and the default gateway.  If no
special cases apply, it outputs the packet.
Table_1 {
	// If the veth sends a broadcast, send it to the gateway and the patch port.
	if reg0=1 && dl_dst=ff:ff:ff:ff:ff:ff {
		output:LOCAL,reg2
	}

	// If the patch port sends a broadcast, send it to the veth.
	if reg0=2 && dl_dst=ff:ff:ff:ff:ff:ff {
		output:reg1
	}

	// If the gateway sends a broadcast, send it to all veths.
	if dl_dst=ff:ff:ff:ff:ff:ff {
		output:veth{1..n}
	}

	// If the veth sends a packet to the gateway, forward it.
	if reg0=1 && dl_dst=gwMac {
		output:LOCAL
	}

	// Drop if a port other than a veth attempts to send to the default gateway.
	if dl_dst=gwMac {
		drop
	}

	// Packets from the gateway don't have the registers set, so use Table_2 to
	// forward based on dl_dst.
	if in_port=LOCAL {
		goto Table_2
	}

	// Send packets from the veth to the patch port.
	if reg0=1 {
		output:reg2
	}

	// Send packets from the patch port to the veth.
	if reg0=2 {
		output:reg1
	}
}

// Table_2 attempts to forward packets to a veth based on its destination MAC.
Table_2 {
	// Packets coming from the
	for each db.Container {
		if nw_dst=dbc.Mac {
			output:veth
		}
	}
}
*/

// A Container that needs OpenFlow rules installed for it.
type Container struct {
	Veth  string
	Patch string
	Mac   string
}

type container struct {
	veth  int
	patch int
	mac   string
}

var staticFlows = []string{
	// Table 0
	"table=0,priority=1000,in_port=LOCAL,actions=resubmit(,1)",

	// Table 1
	"table=1,priority=1000,reg0=0x1,dl_dst=ff:ff:ff:ff:ff:ff," +
		"actions=output:LOCAL,output:NXM_NX_REG2[]",
	"table=1,priority=900,reg0=0x2,dl_dst=ff:ff:ff:ff:ff:ff," +
		"actions=output:NXM_NX_REG1[]",
	fmt.Sprintf("table=1,priority=800,reg0=1,dl_dst=%s,actions=LOCAL",
		ipdef.GatewayMac),
	fmt.Sprintf("table=1,priority=700,dl_dst=%s,actions=drop", ipdef.GatewayMac),
	"table=1,priority=600,in_port=LOCAL,actions=resubmit(,2)",
	"table=1,priority=500,reg0=1,actions=output:NXM_NX_REG2[]",
	"table=1,priority=400,reg0=2,actions=output:NXM_NX_REG1[]",
}

// ReplaceFlows adds flows associated with the provided containers, and removes all
// other flows.
func ReplaceFlows(containers []Container) error {
	ofports, err := openflowPorts()
	if err != nil {
		return err
	}

	flows := allFlows(resolveContainers(ofports, containers))
	if err := ofctl("replace-flows", flows); err != nil {
		return fmt.Errorf("ovs-ofctl: %s", err)
	}

	return nil
}

// AddFlows adds flows associated with the provided containers without touching flows
// that may already be installed.
func AddFlows(containers []Container) error {
	ofports, err := openflowPorts()
	if err != nil {
		return err
	}

	flows := containerFlows(resolveContainers(ofports, containers))
	if err := ofctl("add-flows", flows); err != nil {
		return fmt.Errorf("ovs-ofctl: %s", err)
	}

	return nil
}

func containerFlows(containers []container) []string {
	var flows []string
	for _, c := range containers {
		template := fmt.Sprintf("table=0,priority=1000,in_port=%s%s,"+
			"actions=load:0x%s->NXM_NX_REG0[],load:0x%x->NXM_NX_REG1[],"+
			"load:0x%x->NXM_NX_REG2[],resubmit(,1)",
			"%d", "%s", "%x", c.veth, c.patch)
		flows = append(flows,
			fmt.Sprintf(template, c.veth, ",dl_src="+c.mac, 1),
			fmt.Sprintf(template, c.patch, "", 2),
			fmt.Sprintf("table=2,priority=1000,dl_dst=%s,actions=output:%d",
				c.mac, c.veth))
	}
	return flows
}

func allFlows(containers []container) []string {
	var gatewayBroadcastActions []string
	for _, c := range containers {
		gatewayBroadcastActions = append(gatewayBroadcastActions,
			fmt.Sprintf("output:%d", c.veth))
	}
	flows := append(staticFlows, containerFlows(containers)...)
	return append(flows, "table=1,priority=850,dl_dst=ff:ff:ff:ff:ff:ff,actions="+
		strings.Join(gatewayBroadcastActions, ","))
}

func resolveContainers(portMap map[string]int, containers []Container) []container {
	var ofcs []container
	for _, c := range containers {
		veth, okVeth := portMap[c.Veth]
		patch, okPatch := portMap[c.Patch]
		if !okVeth || !okPatch {
			continue
		}

		ofcs = append(ofcs, container{patch: patch, veth: veth, mac: c.Mac})
	}
	return ofcs
}

func openflowPorts() (map[string]int, error) {
	odb, err := ovsdb.Open()
	if err != nil {
		return nil, fmt.Errorf("ovsdb-server connection: %s", err)
	}
	defer odb.Disconnect()

	return odb.OpenFlowPorts()
}

var ofctl = func(action string, flows []string) error {
	cmd := exec.Command("ovs-ofctl", "-O", "OpenFlow13", action,
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
