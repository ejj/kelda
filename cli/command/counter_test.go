package command

import (
	"bytes"
	"flag"
	"fmt"
	"testing"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/api/pb"
	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/assert"
)

func TestCounterFlags(t *testing.T) {
	t.Parallel()

	counters := &Counters{}

	flags := &flag.FlagSet{}
	assert.Nil(t, flags.Usage)

	counters.InstallFlags(flags)

	assert.NotNil(t, flags.Usage)
}

func TestParse(t *testing.T) {
	t.Parallel()

	counters := &Counters{}
	assert.Error(t, counters.Parse(nil), "")

	assert.NoError(t, counters.Parse([]string{"host"}))
	assert.Equal(t, "host", counters.target)
}

func TestRunQueryDaemon(t *testing.T) {
	t.Parallel()

	counters := &Counters{target: daemonTarget}
	mock := new(mocks.Client)
	counters.client = mock

	mock.On("QueryCounters").Once().Return(nil, assert.AnError)
	assert.NotZero(t, counters.Run())

	mock.On("QueryCounters").Once().Return(nil, nil)
	assert.Zero(t, counters.Run())
}

func TestRunQueryMinion(t *testing.T) {
	t.Parallel()

	counters := &Counters{target: "minion"}
	mock := new(mocks.Client)
	counters.client = mock

	// Test we error when there's no matching machine.
	mock.On("QueryMachines").Once().Return(nil, nil)
	assert.NotZero(t, counters.Run())

	// Test we error when QueryMinionCounters fails.
	mock.On("QueryMachines").Return(
		[]db.Machine{{CloudID: "minion", PublicIP: "host"}}, nil)
	mock.On("QueryMinionCounters", "host").Once().Return(nil, assert.AnError)
	assert.NotZero(t, counters.Run())

	// Test success.
	mock.On("QueryMinionCounters", "host").Once().Return(nil, nil)
	assert.Zero(t, counters.Run())
}

func TestPrintCounters(t *testing.T) {
	t.Parallel()

	counters := []pb.Counter{{
		Pkg:       "PkgA",
		Name:      "NameA",
		Value:     100,
		PrevValue: 56,
	}, {
		Pkg:       "PkgA",
		Name:      "NameBBBBBBBBBBBBBBB",
		Value:     200,
		PrevValue: 200,
	}, {
		Pkg:       "PkgB",
		Name:      "C",
		Value:     300,
		PrevValue: 0}}

	var b bytes.Buffer
	printCounters(&b, counters)
	fmt.Println(b.String())
	assert.Equal(t, `COUNTER                  VALUE  DELTA
                                
PkgA                              
    NameA                100    44
    NameBBBBBBBBBBBBBBB  200    0
PkgB                              
    C                    300    300
`, b.String())
}
