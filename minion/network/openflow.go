package network

import (
	"fmt"

	"github.com/NetSys/quilt/minion/ipdef"
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

	// If the patch port or the gateway sends a broadcast, send it to the veth.
	if dl_dst=ff:ff:ff:ff:ff:ff {
		output:reg1
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

type ofPort struct {
	PatchPort int
	VethPort  int
	Mac       string
}

var staticFlows = []string{
	// Table 0
	"table=0,priority=1000,in_port=LOCAL,actions=resubmit(,1)",

	// Table 1
	"table=1,priority=1000,reg0=0x1,dl_dst=ff:ff:ff:ff:ff:ff," +
		"actions=output:LOCAL,output:NXM_NX_REG2[]",
	"table=1,priority=900,dl_dst=ff:ff:ff:ff:ff:ff,actions=output:NXM_NX_REG1[]",
	fmt.Sprintf("table=1,priority=800,reg0=1,dl_dst=%s,actions=LOCAL",
		ipdef.GatewayMac),
	fmt.Sprintf("table=1,priority=700,dl_dst=%s,actions=drop", ipdef.GatewayMac),
	"table=1,priority=600,in_port=LOCAL,actions=resubmit(,2)",
	"table=1,priority=500,reg0=1,actions=output:NXM_NX_REG2[]",
	"table=1,priority=400,reg0=2,actions=output:NXM_NX_REG1[]",
}

func generateOpenFlow(ofps []ofPort) []string {
	flows := staticFlows
	for _, ofp := range ofps {
		template := fmt.Sprintf("table=0,priority=1000,in_port=%s%s,"+
			"actions=load:0x%s->NXM_NX_REG0[],load:0x%x->NXM_NX_REG1[],"+
			"load:0x%x->NXM_NX_REG2[],resubmit(,1)",
			"%d", "%s", "%x", ofp.VethPort, ofp.PatchPort)
		flows = append(flows,
			fmt.Sprintf(template, ofp.VethPort, ",dl_src="+ofp.Mac, 1),
			fmt.Sprintf(template, ofp.PatchPort, "", 2),
			fmt.Sprintf("table=2,priority=1000,dl_dst=%s,actions=output:%d",
				ofp.Mac, ofp.VethPort))
	}
	return flows
}
