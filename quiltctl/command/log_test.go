package command

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/quilt/quilt/api/client/mocks"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/quiltctl/ssh"
)

func checkLogParsing(t *testing.T, args []string, exp Log, expErr error) {
	logsCmd := NewLogCommand()
	err := parseHelper(logsCmd, args)

	assert.Equal(t, expErr, err)
	assert.Equal(t, exp.target, logsCmd.target)
	assert.Equal(t, exp.privateKey, logsCmd.privateKey)
	assert.Equal(t, exp.sinceTimestamp, logsCmd.sinceTimestamp)
	assert.Equal(t, exp.showTimestamps, logsCmd.showTimestamps)
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
	checkLogParsing(t, []string{"-t", "1"}, Log{
		target:         "1",
		showTimestamps: true,
	}, nil)
	checkLogParsing(t, []string{"--since=07/27/2016", "1"}, Log{
		target:         "1",
		sinceTimestamp: "07/27/2016",
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
		// Show timestamps flag
		{
			cmd: Log{
				target:         targetContainer,
				showTimestamps: true,
			},
			expHost:       "container",
			expSSHCommand: "docker logs --timestamps foo",
		},
		// Since timestamp flag
		{
			cmd: Log{
				target:         targetContainer,
				sinceTimestamp: "2006-01-02T15:04:05",
			},
			expHost:       "container",
			expSSHCommand: "docker logs --since=2006-01-02T15:04:05 foo",
		},
	}

	mockLocalClient := &mocks.Client{
		MachineReturn: []db.Machine{
			{StitchID: targetMachine, PublicIP: "machine"},
		},
	}
	mockGetter := new(mocks.Getter)
	mockGetter.On("Client", mock.Anything).Return(mockLocalClient, nil)
	mockGetter.On("ContainerClient", mock.Anything, mock.Anything).Return(
		&mocks.Client{
			ContainerReturn: []db.Container{
				{
					StitchID: targetContainer,
					DockerID: "foo",
				},
			},
			HostReturn: "container",
		}, nil)

	for _, test := range tests {
		testCmd := test.cmd

		mockSSHClient := new(ssh.MockClient)
		testCmd.sshGetter = func(host, key string) (ssh.Client, error) {
			assert.Equal(t, test.expHost, host)
			assert.Equal(t, "key", key)
			return mockSSHClient, nil
		}
		testCmd.privateKey = "key"
		testCmd.clientGetter = mockGetter
		testCmd.common = &commonFlags{}

		mockSSHClient.On("Run", false, test.expSSHCommand).Return(nil)
		mockSSHClient.On("Close").Return(nil)

		testCmd.Run()

		mockSSHClient.AssertExpectations(t)
	}
}

func TestLogAmbiguousID(t *testing.T) {
	mockClient := &mocks.Client{
		MachineReturn:   []db.Machine{{StitchID: "foo"}},
		ContainerReturn: []db.Container{{StitchID: "foo"}},
	}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
		Return(mockClient, nil)

	testCmd := Log{
		common:       &commonFlags{},
		clientGetter: mockClientGetter,
		target:       "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestLogNoMatch(t *testing.T) {
	mockClient := &mocks.Client{
		MachineReturn:   []db.Machine{{StitchID: "foo"}},
		ContainerReturn: []db.Container{{StitchID: "foo"}},
	}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
		Return(mockClient, nil)

	testCmd := Log{
		common:       &commonFlags{},
		clientGetter: mockClientGetter,
		target:       "bar",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestLogScheduledContainer(t *testing.T) {
	mockClient := &mocks.Client{}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).Return(
		&mocks.Client{
			ContainerReturn: []db.Container{{StitchID: "foo"}},
			HostReturn:      "container",
		}, nil)

	testCmd := Log{
		common:       &commonFlags{},
		clientGetter: mockClientGetter,
		target:       "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}
