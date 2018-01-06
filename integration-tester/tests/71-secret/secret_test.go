package main

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/integration-tester/util"

	"github.com/stretchr/testify/assert"
)

func TestSecretValues(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	assert.Len(t, containers, 1)
	containerID := containers[0].BlueprintID

	// The secret values are defined in the 70-secret-setup test.
	// The non-secret values are defined in the blueprint accompanying this test.
	expEnv := map[string]string{
		"NOT_A_SECRET": "plaintext",
		"MY_SECRET":    "env secret",
	}

	expFiles := map[string]string{
		"/notASecret": "plaintext",
		"/mySecret":   "file secret",
	}

	for key, expVal := range expEnv {
		actualVal, err := getEnv(containerID, key)
		assert.NoError(t, err)

		assert.Equal(t, expVal, actualVal)
		fmt.Printf("Expected environment variable %q to have value %q. Got %q.\n",
			key, expVal, actualVal)
	}

	for path, expVal := range expFiles {
		actualVal, err := getFile(containerID, path)
		assert.NoError(t, err)

		assert.Equal(t, expVal, actualVal)
		fmt.Printf("Expected file %q to have value %q. Got %q.\n",
			path, expVal, actualVal)
	}
}

func getEnv(containerID, key string) (string, error) {
	envBytes, err := exec.Command("kelda", "ssh", containerID,
		"printenv", key).Output()
	if err != nil {
		return "", err
	}

	// Trim the newline appended by printenv.
	return strings.TrimRight(string(envBytes), "\n"), nil
}

func getFile(containerID, path string) (string, error) {
	fileBytes, err := exec.Command("kelda", "ssh", containerID, "cat", path).Output()
	if err != nil {
		return "", err
	}
	return string(fileBytes), nil
}

func TestSecretsEncrypted(t *testing.T) {
	apiClient, creds, err := util.GetDefaultDaemonClient()
	assert.NoError(t, err, "failed to get API client for daemon")
	defer apiClient.Close()

	machines, err := apiClient.QueryMachines()
	assert.NoError(t, err, "failed to query machines")

	leaderIP, err := client.GetLeaderIP(machines, creds)
	assert.NoError(t, err, "failed to find leader")

	sshClient, err := ssh.New(leaderIP, "")
	assert.NoError(t, err, "failed to get SSH client for leader")

	secretPathsBytes, err := sshClient.CombinedOutput(
		"docker exec --env ETCDCTL_API=3 etcd etcdctl get --prefix " +
			"--keys-only /registry/secrets/")
	assert.NoError(t, err, "failed to fetch secret paths")

	secretPaths := strings.Fields(string(secretPathsBytes))
	assert.NotEmpty(t, secretPaths)

	for _, secretPath := range secretPaths {
		secretPathsBytes, err := sshClient.CombinedOutput(
			"docker exec --env ETCDCTL_API=3 etcd etcdctl get " + secretPath)
		assert.NoError(t, err, "failed to fetch secret path %s", secretPath)
		if assert.Contains(t, string(secretPathsBytes), "aescbc") {
			fmt.Println(secretPath + " is encrypted")
		}
	}
}
