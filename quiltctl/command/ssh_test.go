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
	assert.Equal(t, 1, SSH{}.Run())
	assert.Equal(t, 1, SSH{args: []string{"foo"}, allocatePTY: true}.Run())
}

type getMachineTest struct {
	machines   []db.Machine
	query      string
	expErr     string
	expMachine string
}

func TestGetMachine(t *testing.T) {
	t.Parallel()

	tests := []getMachineTest{
		{
			machines: []db.Machine{
				{
					StitchID: "abc",
				},
			},
			query:      "abc",
			expMachine: "abc",
		},
		{
			machines: []db.Machine{
				{
					StitchID: "abc",
				},
				{
					StitchID: "acd",
				},
			},
			query:      "ab",
			expMachine: "abc",
		},
		{
			machines: []db.Machine{
				{
					StitchID: "abc",
				},
				{
					StitchID: "abd",
				},
			},
			query:  "ab",
			expErr: "ambiguous stitchIDs abc and abd",
		},
		{
			machines: []db.Machine{
				{
					StitchID: "abc",
				},
				{
					StitchID: "abd",
				},
			},
			query:  "c",
			expErr: `no machine with stitchID "c"`,
		},
	}
	for _, test := range tests {
		mockClient := mocks.Client{
			MachineReturn: test.machines,
		}
		m, err := getMachine(&mockClient, test.query)

		if test.expErr != "" {
			assert.EqualError(t, err, test.expErr)
			continue
		}

		assert.NoError(t, err)
		assert.Equal(t, test.expMachine, m.StitchID)
	}
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
				common:     &commonFlags{},
				privateKey: "key",
				target:     "tgt",
			},
			machines:    []db.Machine{{StitchID: "tgt", PublicIP: "host"}},
			expHost:     "host",
			expUseShell: true,
		},
		// Machine with exec command.
		{
			cmd: SSH{
				common:     &commonFlags{},
				privateKey: "key",
				target:     "tgt",
				args:       []string{"foo", "bar"},
			},
			machines:   []db.Machine{{StitchID: "tgt", PublicIP: "host"}},
			expHost:    "host",
			expRunArgs: "foo bar",
		},
		// Container with login shell.
		{
			cmd: SSH{
				common:     &commonFlags{},
				privateKey: "key",
				target:     "tgt",
			},
			containers: []db.Container{
				{StitchID: "tgt", DockerID: "dockerID"},
			},
			expAllocatePTY: true,
			expHost:        "host",
			expRunArgs:     "docker exec -it dockerID sh",
		},
		// Container with exec.
		{
			cmd: SSH{
				common:     &commonFlags{},
				privateKey: "key",
				target:     "tgt",
				args:       []string{"foo", "bar"},
			},
			containers: []db.Container{
				{StitchID: "tgt", DockerID: "dockerID"},
			},
			expHost:    "host",
			expRunArgs: "docker exec  dockerID foo bar",
		},
		// Container with exec and PTY.
		{
			cmd: SSH{
				common:      &commonFlags{},
				privateKey:  "key",
				target:      "tgt",
				args:        []string{"foo", "bar"},
				allocatePTY: true,
			},
			containers: []db.Container{
				{StitchID: "tgt", DockerID: "dockerID"},
			},
			expAllocatePTY: true,
			expHost:        "host",
			expRunArgs:     "docker exec -it dockerID foo bar",
		},
	}
	for _, test := range tests {
		testCmd := test.cmd

		mockSSHClient := new(ssh.MockClient)
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

		mockLocalClient := &mocks.Client{
			MachineReturn: test.machines,
		}
		mockContainerClient := &mocks.Client{
			ContainerReturn: test.containers,
			HostReturn:      test.expHost,
		}
		mockClientGetter := new(mocks.Getter)
		mockClientGetter.On("Client", mock.Anything).Return(mockLocalClient, nil)
		mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
			Return(mockContainerClient, nil)
		testCmd.clientGetter = mockClientGetter

		assert.Equal(t, 0, testCmd.Run())
		mockSSHClient.AssertExpectations(t)
	}
}

func TestAmbiguousID(t *testing.T) {
	mockClient := &mocks.Client{
		MachineReturn:   []db.Machine{{StitchID: "foo"}},
		ContainerReturn: []db.Container{{StitchID: "foo"}},
	}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
		Return(mockClient, nil)

	testCmd := SSH{
		common:       &commonFlags{},
		clientGetter: mockClientGetter,
		target:       "foo",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestNoMatch(t *testing.T) {
	mockClient := &mocks.Client{
		MachineReturn:   []db.Machine{{StitchID: "foo"}},
		ContainerReturn: []db.Container{{StitchID: "foo"}},
	}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
		Return(mockClient, nil)

	testCmd := SSH{
		common:       &commonFlags{},
		clientGetter: mockClientGetter,
		target:       "bar",
	}
	assert.Equal(t, 1, testCmd.Run())
}

func TestSSHExitError(t *testing.T) {
	// Test error with exit code.
	mockSSHClient := new(ssh.MockClient)
	mockSSHGetter := func(host string, keyPath string) (ssh.Client, error) {
		return mockSSHClient, nil
	}
	mockSSHClient.On("Close").Return(nil)
	mockSSHClient.On("Run", mock.Anything, mock.Anything).Return(mockExitError(10))

	mockLocalClient := &mocks.Client{
		MachineReturn: []db.Machine{{StitchID: "tgt"}},
	}
	mockClientGetter := new(mocks.Getter)
	mockClientGetter.On("Client", mock.Anything).Return(mockLocalClient, nil)
	mockClientGetter.On("ContainerClient", mock.Anything, mock.Anything).
		Return(nil, errors.New("unused"))

	testCmd := SSH{
		common:       &commonFlags{},
		sshGetter:    mockSSHGetter,
		clientGetter: mockClientGetter,
		target:       "tgt",
		args:         []string{"unused"},
	}
	assert.Equal(t, 10, testCmd.Run())

	// Test error without exit code.
	mockSSHClient = new(ssh.MockClient)
	mockSSHGetter = func(host string, keyPath string) (ssh.Client, error) {
		return mockSSHClient, nil
	}
	mockSSHClient.On("Close").Return(nil)
	mockSSHClient.On("Run", mock.Anything, mock.Anything).Return(errors.New("error"))

	testCmd = SSH{
		common:       &commonFlags{},
		sshGetter:    mockSSHGetter,
		clientGetter: mockClientGetter,
		target:       "tgt",
		args:         []string{"unused"},
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
