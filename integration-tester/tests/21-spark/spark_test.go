package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/kelda/kelda/api/client"
	testerUtil "github.com/kelda/kelda/integration-tester/util"
)

func TestRunShuffleJob(t *testing.T) {
	clnt, _, err := testerUtil.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	containersPretty, _ := exec.Command("kelda", "ps").Output()
	fmt.Println("`kelda ps` output:")
	fmt.Println(string(containersPretty))

	// Run a job that shuffles 100,000 key-value pairs using 100 mappers and 10
	// reducers.
	numMappers := 100
	keyValuePairsPerMapper := 1000
	valuesPerKey := 4
	numReducers := 10
	cmd := exec.Command("kelda", "ssh", "spark-driver", "run-example",
		fmt.Sprintf("GroupByTest %d %d %d %d",
			numMappers, keyValuePairsPerMapper, valuesPerKey, numReducers))
	stderrBytes := bytes.NewBuffer(nil)
	cmd.Stderr = stderrBytes

	stdoutBytes, err := cmd.Output()
	fmt.Println(stderrBytes.String())
	fmt.Println(string(stdoutBytes))
	if err != nil {
		t.Fatalf("Failed to run spark job: %s", err.Error())
	}

	checkForLostTasks(t, stderrBytes.String())

	// Make sure that the job ran on multiple machines, instead of just on the
	// master, by checking for log messages that contain "localhost" (which will
	// be present when the job runs in local mode). This test assumes that INFO-level
	// logging is enabled, because the potential log messages containing "localhost"
	// are at INFO level.
	if strings.Contains(stderrBytes.String(), "localhost") {
		t.Fatalf("Spark job expected to run in distributed mode, but it ran " +
			"in local mode")
	}

	outputWorkerLogs(t, clnt)

	outputAsStr := strings.Trim(string(stdoutBytes), " \n\t")
	outputAsNum, err := strconv.Atoi(outputAsStr)
	if err != nil {
		t.Fatalf("Output of Spark job was expected to be a number (was %s)",
			outputAsStr)
	}

	// The expected output is the number of unique keys.  Since the keys are randomly
	// generated, there will be some overlap in the keys, so there won't quite be
	// numMappers*numKeyValuePairsPerMapper (i.e., 100,000) unique keys.
	maxOutput := numMappers * keyValuePairsPerMapper
	minOutput := int(float64(maxOutput) * 0.99)
	if outputAsNum < minOutput || outputAsNum > maxOutput {
		t.Fatalf("Spark shuffle job result (%d) was not in the expected range",
			outputAsNum)
	}
}

func TestReadFromS3(t *testing.T) {
	clnt, _, err := testerUtil.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	// Run a job that reads a file from S3.
	cmd := exec.Command("kelda", "ssh", "spark-driver", "run-example",
		fmt.Sprintf("HdfsTest s3a://kelda-spark-test/sonnets.txt"))

	outBytes, err := cmd.CombinedOutput()
	fmt.Println(string(outBytes))
	if err != nil {
		t.Fatalf("Failed to run Spark job: %s", err.Error())
	}

	checkForLostTasks(t, string(outBytes))
	outputWorkerLogs(t, clnt)
}

// checkForLostTasks looks for lost tasks in the Spark output (this catches a case
// where a task failed, but then was re-run successfully on a different executor,
// in which case the Spark job will complete successfully and the tests may pass in
// spite of a problem that occurred).
func checkForLostTasks(t *testing.T, output string) {
	if strings.Contains(output, "Lost task") {
		t.Fatalf("Lost task found in Spark job output; no task failures " +
			"should occur during this test. Run tests with -v to see job " +
			"output.")
	}
}

// outputWorkerLogs prints the logs from all of the workers (Spark logs this output in a
// special file, so it's not automatically included in the logs that Jenkins saves after
// running each test).
func outputWorkerLogs(t *testing.T, clnt client.Client) {
	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("Failed to query containers: %s", err.Error())
	}

	failedToGetLogs := false
	for _, container := range containers {
		if !strings.Contains(container.Hostname, "spark-worker") {
			continue
		}
		// Get the most recent Spark application that ran (this handles the case
		// where a user runs the integration tests multiple times, using the
		// same set of containers).
		cmd := exec.Command("kelda", "ssh", container.Hostname,
			"ls", "-t", "spark/work")
		outAndErrBytes, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Failed to list logs in spark/work on %s: %s\n%s",
				container.Hostname, outAndErrBytes, err)
			failedToGetLogs = true
			continue
		}
		appName := strings.Split(
			strings.Trim(string(outAndErrBytes), " \n\t"),
			"\n")[0]
		// Print the logs for all executors for the application (there may be
		// multiple if some failed and the driver re-started them). This command
		// needs to use bash because the default shell doesn't evaluate the
		// wildcard correctly.
		catCmd := fmt.Sprintf(`bash -c "cat spark/work/%s/*/stderr"`, appName)
		cmd = exec.Command("kelda", "ssh", container.Hostname, catCmd)
		outAndErrBytes, err = cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Failed to get worker logs on %s: %s\n%s",
				container.Hostname, outAndErrBytes, err.Error())
			failedToGetLogs = true
			continue
		}
		fmt.Printf("Spark log output for worker %s:\n%s\n", container.Hostname,
			outAndErrBytes)
	}

	if failedToGetLogs {
		t.Fatalf("Failed to get logs from one or more workers (see errors " +
			"above).")
	}
}
