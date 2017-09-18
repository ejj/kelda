package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/cli/ssh"
	mockSSH "github.com/kelda/kelda/cli/ssh/mocks"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
)

var debugFolder = "debug_logs_Mon_Jan_01_00-00-00"

func checkDebugParsing(t *testing.T, args []string, expArgs Debug, expErrMsg string) {
	debugCmd := NewDebugCommand()
	err := parseHelper(debugCmd, args)

	if expErrMsg != "" {
		assert.EqualError(t, err, expErrMsg)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, expArgs.all, debugCmd.all)
	assert.Equal(t, expArgs.containers, debugCmd.containers)
	assert.Equal(t, expArgs.machines, debugCmd.machines)
	assert.Equal(t, expArgs.privateKey, debugCmd.privateKey)
	assert.Equal(t, expArgs.ids, debugCmd.ids)
}

func TestDebugFlags(t *testing.T) {
	t.Parallel()

	checkDebugParsing(t, []string{"-i", "key", "1"},
		Debug{
			privateKey: "key",
			tar:        true,
			ids:        []string{"1"},
		}, "")
	checkDebugParsing(t, []string{"-i", "key", "-all"},
		Debug{
			privateKey: "key",
			tar:        true,
			all:        true,
			ids:        []string{},
		}, "")
	checkDebugParsing(t, []string{"-i", "key", "-containers"},
		Debug{
			privateKey: "key",
			tar:        true,
			containers: true,
			ids:        []string{},
		}, "")
	checkDebugParsing(t, []string{"-i", "key", "-machines"},
		Debug{
			privateKey: "key",
			tar:        true,
			machines:   true,
			ids:        []string{},
		}, "")
	checkDebugParsing(t, []string{"-i", "key", "id1", "id2"},
		Debug{
			privateKey: "key",
			tar:        true,
			ids:        []string{"id1", "id2"},
		}, "")
	checkDebugParsing(t, []string{"-all", "-machines", "id1", "id2"},
		Debug{
			tar:      true,
			all:      true,
			machines: true,
			ids:      []string{"id1", "id2"},
		}, "")
	checkDebugParsing(t, []string{"-containers", "-machines", "id1", "id2"},
		Debug{
			tar:        true,
			containers: true,
			machines:   true,
			ids:        []string{"id1", "id2"},
		}, "")
	checkDebugParsing(t, []string{"-tar=false", "-machines"},
		Debug{
			tar:      false,
			machines: true,
			ids:      []string{},
		}, "")
	checkDebugParsing(t, []string{"-o=tmp_folder", "-machines"},
		Debug{
			tar:      true,
			outPath:  "tmp_folder",
			machines: true,
			ids:      []string{},
		}, "")
	checkDebugParsing(t, []string{}, Debug{},
		"must supply at least one ID or set option")
	checkDebugParsing(t, []string{"-i", "key"}, Debug{},
		"must supply at least one ID or set option")
}

type debugTest struct {
	cmd        Debug
	machines   []db.Machine
	containers []db.Container

	expSSH    bool
	expReturn int
	expFiles  []string
}

func TestDebug(t *testing.T) {
	timestamp = func() time.Time {
		return time.Time{}
	}
	defer func() {
		timestamp = time.Now
	}()

	execCmd = func(name string, arg ...string) *exec.Cmd {
		assert.Equal(t, name, "quilt")
		return exec.Command("echo", "hello world")
	}
	defer func() {
		execCmd = exec.Command
	}()

	tests := []debugTest{
		// Check that all logs are fetched.
		{
			cmd: Debug{
				tar: false,
				all: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
				{
					CloudID:   "4",
					PublicIP:  "8.8.8.8",
					PrivateIP: "9.9.9.9",
					Role:      db.Master,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles(debugFolder, "1"),
				masterMachineFiles(debugFolder, "4"),
				containerFiles(debugFolder, "2"),
				containerFiles(debugFolder, "3"),
				daemonFiles(debugFolder)),
		},
		// Check that all logs are fetched with -machines and -containers.
		{
			cmd: Debug{
				tar:        false,
				machines:   true,
				containers: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles(debugFolder, "1"),
				containerFiles(debugFolder, "2"),
				containerFiles(debugFolder, "3"),
				daemonFiles(debugFolder)),
		},
		// Check that just container logs are fetched.
		{
			cmd: Debug{
				tar:        false,
				containers: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(containerFiles(debugFolder, "2"),
				containerFiles(debugFolder, "3"),
				daemonFiles(debugFolder)),
		},
		// Check that just machine logs are fetched.
		{
			cmd: Debug{
				tar:      false,
				machines: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
				{
					CloudID:   "4",
					PublicIP:  "5.6.7.8",
					PrivateIP: "8.7.6.5",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "8.7.6.5"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles(debugFolder, "1"),
				workerMachineFiles(debugFolder, "4"),
				daemonFiles(debugFolder)),
		},
		// Check that we can get logs by specific blueprint ids
		{
			cmd: Debug{
				tar: false,
				ids: []string{"2", "4", "5"},
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
				{BlueprintID: "4", DockerID: "c", Minion: "4.3.2.1"},
				{BlueprintID: "5", DockerID: "d", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(containerFiles(debugFolder, "2"),
				containerFiles(debugFolder, "4"),
				containerFiles(debugFolder, "5"),
				daemonFiles(debugFolder)),
		},
		// Check that we can get logs by specific blueprint ids in arbitrary order
		{
			cmd: Debug{
				tar: false,
				ids: []string{"4", "2", "1"},
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
				{BlueprintID: "4", DockerID: "c", Minion: "4.3.2.1"},
				{BlueprintID: "5", DockerID: "d", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles(debugFolder, "1"),
				containerFiles(debugFolder, "4"),
				containerFiles(debugFolder, "2"),
				daemonFiles(debugFolder)),
		},
		// Check that we error on arbitrary blueprint IDs.
		{
			cmd: Debug{
				tar: false,
				ids: []string{"4", "2"},
			},
			machines: []db.Machine{
				{
					CloudID:   "409",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
				{BlueprintID: "41", DockerID: "c", Minion: "4.3.2.1"},
				{BlueprintID: "5", DockerID: "d", Minion: "4.3.2.1"},
			},
			expSSH:    false,
			expReturn: 1,
		},
		// Check that we error on non-existent blueprint IDs.
		{
			cmd: Debug{
				tar: false,
				ids: []string{"6"},
			},
			machines: []db.Machine{
				{
					CloudID:   "409",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
				{BlueprintID: "41", DockerID: "c", Minion: "4.3.2.1"},
				{BlueprintID: "5", DockerID: "d", Minion: "4.3.2.1"},
			},
			expSSH:    false,
			expReturn: 1,
		},
		// Check that containers without a minion aren't reported.
		{
			cmd: Debug{
				tar:        false,
				containers: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
				{BlueprintID: "4", DockerID: "c", Minion: "4.3.2.1"},
				{BlueprintID: "5", DockerID: "d", Minion: ""},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(containerFiles(debugFolder, "2"),
				containerFiles(debugFolder, "3"),
				containerFiles(debugFolder, "4"),
				daemonFiles(debugFolder)),
		},
		// Check that machines without an IP aren't reported.
		{
			cmd: Debug{
				tar:      false,
				machines: true,
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
				{
					CloudID:   "4",
					PublicIP:  "",
					PrivateIP: "",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles(debugFolder, "1"),
				daemonFiles(debugFolder)),
		},
		// Check that a supplied path is respected.
		{
			cmd: Debug{
				tar:     false,
				all:     true,
				outPath: "tmp_folder",
			},
			machines: []db.Machine{
				{
					CloudID:   "1",
					PublicIP:  "1.2.3.4",
					PrivateIP: "4.3.2.1",
					Role:      db.Worker,
				},
			},
			containers: []db.Container{
				{BlueprintID: "2", DockerID: "a", Minion: "4.3.2.1"},
				{BlueprintID: "3", DockerID: "b", Minion: "4.3.2.1"},
			},
			expSSH:    true,
			expReturn: 0,
			expFiles: flatten(workerMachineFiles("tmp_folder", "1"),
				containerFiles("tmp_folder", "2"),
				containerFiles("tmp_folder", "3"),
				daemonFiles("tmp_folder")),
		},
	}

	for _, test := range tests {
		util.AppFs = afero.NewMemMapFs()
		testCmd := test.cmd

		mockSSHClient := new(mockSSH.Client)
		testCmd.sshGetter = func(host string, keyPath string) (
			ssh.Client, error) {

			assert.Equal(t, testCmd.privateKey, keyPath)
			return mockSSHClient, nil
		}
		if test.expSSH {
			mockSSHClient.On("CombinedOutput",
				mock.Anything).Return([]byte(""), nil)
		}

		mockLocalClient := new(mocks.Client)
		mockLocalClient.On("QueryMachines").Return(test.machines, nil)
		mockLocalClient.On("QueryContainers").Return(test.containers, nil)
		mockLocalClient.On("Close").Return(nil)
		testCmd.connectionHelper = connectionHelper{
			client: mockLocalClient,
		}

		assert.Equal(t, test.expReturn, testCmd.Run())
		rootDir := debugFolder
		if test.cmd.outPath != "" {
			rootDir = test.cmd.outPath
		}

		// There should only be daemon files if the fetch succeeded and we didn't
		// tarball the results.
		if test.expReturn == 0 && !test.cmd.tar {
			for _, cmd := range daemonCmds {
				file := filepath.Join(rootDir, cmd.name)
				exists, err := util.FileExists(file)
				assert.NoError(t, err)
				assert.True(t, exists)
			}
		}

		actualFiles, err := listFiles()
		assert.NoError(t, err)
		sort.Strings(actualFiles)
		expFiles := test.expFiles
		sort.Strings(expFiles)
		assert.Equal(t, expFiles, actualFiles)

		mockSSHClient.AssertExpectations(t)
	}
}

func listFiles() (files []string, err error) {
	err = afero.Afero{Fs: util.AppFs}.Walk("",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() {
				files = append(files, path)
			}
			return nil
		},
	)
	return files, err
}

func containerFiles(rootDir, id string) []string {
	return withParentFolder(rootDir, containerDir, id, containerCmds)
}

func commonMachineFiles(rootDir, id string) []string {
	return withParentFolder(rootDir, machineDir, id, machineCmds)
}

func masterMachineFiles(rootDir, id string) []string {
	return append(commonMachineFiles(rootDir, id),
		withParentFolder(rootDir, machineDir, id, masterMachineCmds)...)
}

func workerMachineFiles(rootDir, id string) []string {
	return append(commonMachineFiles(rootDir, id),
		withParentFolder(rootDir, machineDir, id, workerMachineCmds)...)
}

func daemonFiles(rootDir string) (exp []string) {
	for _, cmd := range daemonCmds {
		exp = append(exp, filepath.Join(rootDir, cmd.name))
	}
	return exp
}

func withParentFolder(rootDir, typeDir, id string, cmds []logCmd) (exp []string) {
	for _, cmd := range cmds {
		exp = append(exp, filepath.Join(rootDir, typeDir, id, cmd.name))
	}
	return exp
}

func flatten(fileLists ...[]string) (files []string) {
	for _, lst := range fileLists {
		files = append(files, lst...)
	}
	return files
}
