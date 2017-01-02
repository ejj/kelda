package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateOpenFlow(t *testing.T) {
	t.Parallel()
	flows := generateOpenFlow([]ofPort{
		{PatchPort: 4, VethPort: 5, Mac: "66:66:66:66:66:66"},
		{PatchPort: 9, VethPort: 8, Mac: "99:99:99:99:99:99"}})
	exp := append(staticFlows,
		"table=0,priority=1000,in_port=5,dl_src=66:66:66:66:66:66,"+
			"actions=load:0x1->NXM_NX_REG0[],load:0x5->NXM_NX_REG1[],"+
			"load:0x4->NXM_NX_REG2[],resubmit(,1)",
		"table=0,priority=1000,in_port=4,"+
			"actions=load:0x2->NXM_NX_REG0[],load:0x5->NXM_NX_REG1[],"+
			"load:0x4->NXM_NX_REG2[],resubmit(,1)",
		"table=2,priority=1000,dl_dst=66:66:66:66:66:66,actions=output:5",
		"table=0,priority=1000,in_port=8,dl_src=99:99:99:99:99:99,"+
			"actions=load:0x1->NXM_NX_REG0[],load:0x8->NXM_NX_REG1[],"+
			"load:0x9->NXM_NX_REG2[],resubmit(,1)",
		"table=0,priority=1000,in_port=9,"+
			"actions=load:0x2->NXM_NX_REG0[],load:0x8->NXM_NX_REG1[],"+
			"load:0x9->NXM_NX_REG2[],resubmit(,1)",
		"table=2,priority=1000,dl_dst=99:99:99:99:99:99,actions=output:8")
	assert.Equal(t, exp, flows)
}
