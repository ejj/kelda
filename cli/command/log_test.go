package command

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/cli/ssh"
	mockSSH "github.com/kelda/kelda/cli/ssh/mocks"
	"github.com/kelda/kelda/db"
)

func checkLogParsing(t *testing.T, args []string, exp Log, expErr error) {
	logsCmd := NewLogCommand()
	err := parseHelper(logsCmd, args)

	assert.Equal(t, expErr, err)
	assert.Equal(t, exp.target, logsCmd.target)
	assert.Equal(t, exp.privateKey, logsCmd.privateKey)
	assert.Equal(t, exp.shouldTail, logsCmd.shouldTail)
}

func TestLogFlags(t *testing.T) {
	t.Parallel()

	checkLogParsing(t, []string{"1"}, Log{
		target: "1",
	}, nil)
	checkLogParsing(t, []string{"-i", "key", "1"}, Log{
		target:     "1",
		privateKey: "key",
	}, nil)
	checkLogParsing(t, []string{"-f", "1"}, Log{
		target:     "1",
		shouldTail: true,
	}, nil)
	checkLogParsing(t, []string{}, Log{},
		errors.New("must specify a target container or machine"))
}

type logTest struct {
	cmd           Log
	expHost       string
	expSSHCommand string
}

func TestLog(t *testing.T) {
	t.Parallel()

	targetContainer := "1"
	targetMachine := "a"

	tests := []logTest{
		// Target container.
		{
			cmd:           Log{target: targetContainer},
			expHost:       "container",
			expSSHCommand: "docker logs foo",
		},
		// Target machine.
		{
			cmd:           Log{target: targetMachine},
			expHost:       "machine",
			expSSHCommand: "docker logs minion",
		},
		// Tail flag
		{
			cmd: Log{
				target:     targetContainer,
				shouldTail: true,
			},
			expHost:       "container",
			expSSHCommand: "docker logs --follow foo",
		},
	}

	mockLocalClient := new(mocks.Client)
	mockLocalClient.On("QueryMachines").Return([]db.Machine{{
		CloudID:  targetMachine,
		PublicIP: "machine",
	}, {
		PublicIP:  "container",
		PrivateIP: "containerPriv",
	}}, nil)
	mockLocalClient.On("QueryContainers").Return([]db.Container{{
		BlueprintID: targetContainer,
		DockerID:    "foo",
		Minion:      "containerPriv",
	}}, nil)
	mockLocalClient.On("Close").Return(nil)

	for _, test := range tests {
		testCmd := test.cmd

		mockSSHClient := new(mockSSH.Client)
		testCmd.sshGetter = func(host, key string) (ssh.Client, error) {
			assert.Equal(t, test.expHost, host)
			assert.Equal(t, "key", key)
			return mockSSHClient, nil
		}
		testCmd.privateKey = "key"
		testCmd.connectionHelper = connectionHelper{client: mockLocalClient}

		mockSSHClient.On("Run", false, test.expSSHCommand).Return(nil)
		mockSSHClient.On("Close").Return(nil)

		testCmd.Run()

		mockSSHClient.AssertExpectations(t)
	}
}

func TestLogAmbiguousID(t *testing.T) {
	mockClient := new(mocks.Client)
	mockClient.On("QueryMachines").Return([]db.Machine{{
		CloudID: "foo",
	}}, nil)
	mockClient.On("QueryContainers").Return([]db.Container{{
		BlueprintID: "foo",
	}}, nil)
	mockClient.On("Close").Return(nil)

	testCmd := Log{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestLogNoMatch(t *testing.T) {
	mockClient := new(mocks.Client)
	mockClient.On("QueryMachines").Return([]db.Machine{{
		CloudID: "foo",
	}}, nil)
	mockClient.On("QueryContainers").Return([]db.Container{{
		BlueprintID: "foo",
	}}, nil)
	mockClient.On("Close").Return(nil)

	testCmd := Log{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "bar",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestLogScheduledContainer(t *testing.T) {
	mockClient := new(mocks.Client)
	mockClient.On("QueryContainers").Return([]db.Container{{
		BlueprintID: "foo",
	}}, nil)
	mockClient.On("QueryMachines").Return(nil, nil)
	mockClient.On("Close").Return(nil)

	testCmd := Log{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}
