package command

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/cli/ssh"
	mockSSH "github.com/kelda/kelda/cli/ssh/mocks"
	"github.com/kelda/kelda/db"
)

func checkSSHParsing(t *testing.T, args []string, expArgs SSH, expErrMsg string) {
	sshCmd := NewSSHCommand()
	err := parseHelper(sshCmd, args)

	if expErrMsg != "" {
		assert.EqualError(t, err, expErrMsg)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, expArgs.target, sshCmd.target)
	assert.Equal(t, expArgs.args, sshCmd.args)
	assert.Equal(t, expArgs.allocatePTY, sshCmd.allocatePTY)
	assert.Equal(t, expArgs.privateKey, sshCmd.privateKey)
}

func TestSSHFlags(t *testing.T) {
	t.Parallel()

	checkSSHParsing(t, []string{"-i", "key", "1"},
		SSH{
			target:     "1",
			privateKey: "key",
			args:       []string{},
		}, "")
	checkSSHParsing(t, []string{"-i", "key", "1"},
		SSH{
			target:     "1",
			privateKey: "key",
			args:       []string{},
		}, "")
	checkSSHParsing(t, []string{"-i", "key", "1", "arg1", "arg2"},
		SSH{
			target:     "1",
			privateKey: "key",
			args:       []string{"arg1", "arg2"},
		}, "")
	checkSSHParsing(t, []string{"-i", "key", "1", "arg1", "arg2"},
		SSH{
			target:     "1",
			privateKey: "key",
			args:       []string{"arg1", "arg2"},
		}, "")
	checkSSHParsing(t, []string{"-i", "key", "-t", "1", "arg1", "arg2"},
		SSH{
			target:      "1",
			privateKey:  "key",
			args:        []string{"arg1", "arg2"},
			allocatePTY: true,
		}, "")

	checkSSHParsing(t, []string{}, SSH{}, "must specify a target")
	checkSSHParsing(t, []string{"-i", "key"}, SSH{},
		"must specify a target")
}

func TestSSHPTY(t *testing.T) {
	isTerminal = func() bool { return false }
	assert.Equal(t, 1, SSH{
		connectionHelper: connectionHelper{client: &mocks.Client{}},
	}.Run())
	assert.Equal(t, 1, SSH{
		connectionHelper: connectionHelper{client: &mocks.Client{}},
		args:             []string{"foo"},
		allocatePTY:      true,
	}.Run())
}

type sshTest struct {
	cmd            SSH
	machines       []db.Machine
	containers     []db.Container
	expHost        string
	expUseShell    bool
	expRunArgs     string
	expAllocatePTY bool
}

func TestSSH(t *testing.T) {
	isTerminal = func() bool { return true }
	tests := []sshTest{
		// Machine with login shell.
		{
			cmd: SSH{
				privateKey: "key",
				target:     "tgt",
			},
			machines:    []db.Machine{{CloudID: "tgt", PublicIP: "host"}},
			expHost:     "host",
			expUseShell: true,
		},
		// Machine with exec command.
		{
			cmd: SSH{
				privateKey: "key",
				target:     "tgt",
				args:       []string{"foo", "bar"},
			},
			machines:   []db.Machine{{CloudID: "tgt", PublicIP: "host"}},
			expHost:    "host",
			expRunArgs: "foo bar",
		},
		// Container with login shell.
		{
			cmd: SSH{
				privateKey: "key",
				target:     "tgt",
			},
			machines: []db.Machine{{PrivateIP: "priv", PublicIP: "host"}},
			containers: []db.Container{{
				Minion:      "priv",
				BlueprintID: "tgt",
				DockerID:    "dockerID",
			}},
			expAllocatePTY: true,
			expHost:        "host",
			expRunArgs:     "docker exec -it dockerID sh",
		},
		// Container with exec.
		{
			cmd: SSH{
				privateKey: "key",
				target:     "tgt",
				args:       []string{"foo", "bar"},
			},
			machines: []db.Machine{{PrivateIP: "priv", PublicIP: "host"}},
			containers: []db.Container{{
				Minion:      "priv",
				BlueprintID: "tgt",
				DockerID:    "dockerID",
			}},
			expHost:    "host",
			expRunArgs: "docker exec  dockerID foo bar",
		},
		// Container with exec and PTY.
		{
			cmd: SSH{
				privateKey:  "key",
				target:      "tgt",
				args:        []string{"foo", "bar"},
				allocatePTY: true,
			},
			machines: []db.Machine{{PrivateIP: "priv", PublicIP: "host"}},
			containers: []db.Container{{
				Minion:      "priv",
				BlueprintID: "tgt",
				DockerID:    "dockerID",
			}},
			expAllocatePTY: true,
			expHost:        "host",
			expRunArgs:     "docker exec -it dockerID foo bar",
		},
	}
	for _, test := range tests {
		testCmd := test.cmd

		mockSSHClient := new(mockSSH.Client)
		testCmd.sshGetter = func(host string, keyPath string) (
			ssh.Client, error) {
			assert.Equal(t, test.expHost, host)
			assert.Equal(t, testCmd.privateKey, keyPath)
			return mockSSHClient, nil
		}
		mockSSHClient.On("Close").Return(nil)
		if test.expUseShell {
			mockSSHClient.On("Shell").Return(nil)
		} else {
			mockSSHClient.On("Run", test.expAllocatePTY, test.expRunArgs).
				Return(nil)
		}

		mockClient := new(mocks.Client)
		mockClient.On("QueryMachines").Return(test.machines, nil)
		mockClient.On("QueryContainers").Return(test.containers, nil)
		mockClient.On("Close").Return(nil)

		testCmd.connectionHelper = connectionHelper{client: mockClient}

		assert.Equal(t, 0, testCmd.Run())
		mockSSHClient.AssertExpectations(t)
	}
}

func TestAmbiguousID(t *testing.T) {
	mockClient := new(mocks.Client)
	mockClient.On("QueryMachines").Return([]db.Machine{{CloudID: "foo"}}, nil)
	mockClient.On("QueryContainers").Return([]db.Container{{BlueprintID: "foo"}}, nil)
	mockClient.On("Close").Return(nil)

	testCmd := SSH{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestNoMatch(t *testing.T) {
	mockClient := new(mocks.Client)
	mockClient.On("QueryMachines").Return([]db.Machine{{CloudID: "foo"}}, nil)
	mockClient.On("QueryContainers").Return([]db.Container{{BlueprintID: "foo"}}, nil)
	mockClient.On("Close").Return(nil)

	testCmd := SSH{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "bar",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestSSHExitError(t *testing.T) {
	// Test error with exit code.
	mockSSHClient := new(mockSSH.Client)
	mockSSHGetter := func(host string, keyPath string) (ssh.Client, error) {
		return mockSSHClient, nil
	}
	mockSSHClient.On("Close").Return(nil)
	mockSSHClient.On("Run", mock.Anything, mock.Anything).Return(mockExitError(10))

	mockClient := new(mocks.Client)
	mockClient.On("QueryMachines").Return([]db.Machine{{CloudID: "tgt"}}, nil)
	mockClient.On("QueryContainers").Return(nil, nil)
	mockClient.On("Close").Return(nil)

	testCmd := SSH{
		connectionHelper: connectionHelper{client: mockClient},
		sshGetter:        mockSSHGetter,
		target:           "tgt",
		args:             []string{"unused"},
	}
	assert.Equal(t, 10, testCmd.Run())

	// Test error without exit code.
	mockSSHClient = new(mockSSH.Client)
	mockSSHGetter = func(host string, keyPath string) (ssh.Client, error) {
		return mockSSHClient, nil
	}
	mockSSHClient.On("Close").Return(nil)
	mockSSHClient.On("Run", mock.Anything, mock.Anything).Return(errors.New("error"))

	testCmd = SSH{
		connectionHelper: connectionHelper{client: mockClient},
		sshGetter:        mockSSHGetter,
		target:           "tgt",
		args:             []string{"unused"},
	}
	assert.Equal(t, 1, testCmd.Run())
}

type mockExitError int

func (err mockExitError) Error() string {
	return "error"
}

func (err mockExitError) ExitStatus() int {
	return int(err)
}

func TestSSHScheduledContainer(t *testing.T) {
	mockClient := &mocks.Client{}
	mockClient.On("QueryContainers").Return([]db.Container{{BlueprintID: "foo"}}, nil)
	mockClient.On("QueryMachines").Return(nil, nil)
	mockClient.On("Close").Return(nil)

	testCmd := SSH{
		connectionHelper: connectionHelper{client: mockClient},
		target:           "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}
