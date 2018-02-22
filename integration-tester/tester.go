package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/kelda/kelda/blueprint"
	cliPath "github.com/kelda/kelda/cli/path"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	testerUtil "github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util"
)

// The global logger for this CI run.
var log logger

func main() {
	namespace := os.Getenv("TESTING_NAMESPACE")
	if namespace == "" {
		logrus.Error("Please set TESTING_NAMESPACE.")
		os.Exit(1)
	}

	var err error
	if log, err = newLogger(); err != nil {
		logrus.WithError(err).Error("Failed to create logger.")
		os.Exit(1)
	}

	tester, err := newTester(namespace)
	if err != nil {
		logrus.WithError(err).Error("Failed to create tester instance.")
		os.Exit(1)
	}

	if err := tester.run(); err != nil {
		logrus.WithError(err).Error("Test execution failed.")
		os.Exit(1)
	}
}

type tester struct {
	exitOnFirstFailure bool
	preserveFailed     bool
	junitOutDir        string

	testSuites  []*testSuite
	initialized bool
	namespace   string
}

func newTester(namespace string) (tester, error) {
	t := tester{
		namespace: namespace,
	}

	testRoot := flag.String("testRoot", "",
		"the root directory containing the integration tests")
	flag.BoolVar(&t.preserveFailed, "preserve-failed", false,
		"don't destroy machines on failed tests")
	flag.BoolVar(&t.exitOnFirstFailure, "exit-on-first-failure", false,
		"immediately exit if a test suite fails")
	flag.StringVar(&t.junitOutDir, "junitOut", "",
		"directory to write junit reports")
	flag.Parse()

	if *testRoot == "" {
		return tester{}, errors.New("testRoot is required")
	}

	if t.junitOutDir == "" {
		return tester{}, errors.New("junitOut is required")
	}

	err := afero.Afero{Fs: util.AppFs}.MkdirAll(t.junitOutDir, 0755)
	if err != nil {
		return tester{}, fmt.Errorf(
			"failed to create junit output directory: %s", err)
	}

	err = t.generateTestSuites(*testRoot)
	if err != nil {
		return tester{}, err
	}

	return t, nil
}

func (t *tester) generateTestSuites(testRoot string) error {
	l := log.testerLogger

	// First, we need to ls the testRoot, and find all of the folders. Then we can
	// generate a testSuite for each folder.
	testSuiteFolders, err := filepath.Glob(filepath.Join(testRoot, "*"))
	if err != nil {
		l.infoln("Could not access test suite folders")
		l.errorln(err.Error())
		return err
	}

	sort.Sort(byPriorityPrefix(testSuiteFolders))
	for _, testSuiteFolder := range testSuiteFolders {
		files, err := ioutil.ReadDir(testSuiteFolder)
		if err != nil {
			l.infoln(fmt.Sprintf(
				"Error reading test suite %s", testSuiteFolder))
			l.errorln(err.Error())
			return err
		}

		var blueprint, test string
		for _, file := range files {
			path := filepath.Join(testSuiteFolder, file.Name())
			switch {
			case strings.HasSuffix(file.Name(), ".js"):
				blueprint = path
			// If the file is executable by everyone, and is not a directory.
			case (file.Mode()&1 != 0) && !file.IsDir():
				test = path
			}
		}

		name := filepath.Base(testSuiteFolder)
		newSuite := testSuite{
			name:         name,
			blueprint:    "./" + blueprint,
			test:         test,
			junitOutFile: filepath.Join(t.junitOutDir, name+".xml"),
		}
		t.testSuites = append(t.testSuites, &newSuite)
	}

	return nil
}

func (t tester) run() error {
	defer func() {
		failed := false
		for _, result := range t.testSuites {
			if !result.passed {
				failed = true
				break
			}
		}

		if failed && t.preserveFailed {
			return
		}

		stop(t.namespace)
	}()

	if err := t.setup(); err != nil {
		log.testerLogger.errorln("Unable to setup the tests, bailing.")
		// All suites failed if we didn't run them.
		for _, suite := range t.testSuites {
			suite.passed = false
		}
		return err
	}

	return t.runTestSuites()
}

func (t *tester) setup() error {
	l := log.testerLogger

	l.infoln("Starting the Kelda daemon.")
	go runKeldaDaemon()

	// Get blueprint dependencies.
	l.infoln("Installing blueprint dependencies")
	_, _, err := npmInstall()
	if err != nil {
		l.infoln("Could not install dependencies")
		l.errorln(err.Error())
		return err
	}

	// Wait for the daemon to generate the TLS credentials. If we don't wait,
	// the subsequent Kelda commands (such as `kelda stop`) might fail if they
	// are executed before the credentials are generated.
	err = util.BackoffWaitFor(func() bool {
		_, caCertErr := util.Stat(tlsIO.CACertPath(cliPath.DefaultTLSDir))
		_, caKeyErr := util.Stat(tlsIO.CAKeyPath(cliPath.DefaultTLSDir))
		_, signedCertErr := util.Stat(tlsIO.SignedCertPath(cliPath.DefaultTLSDir))
		_, signedKeyErr := util.Stat(tlsIO.SignedKeyPath(cliPath.DefaultTLSDir))
		return caCertErr == nil && caKeyErr == nil &&
			signedCertErr == nil && signedKeyErr == nil
	}, 15*time.Second, 3*time.Minute)
	if err != nil {
		l.infoln("Timed out waiting for daemon to generate TLS credentials")
		return err
	}

	// Do a preliminary kelda stop.
	l.infoln(fmt.Sprintf("Preliminary `kelda stop %s`", t.namespace))
	_, _, err = stop(t.namespace)
	if err != nil {
		l.infoln(fmt.Sprintf("Error stopping: %s", err.Error()))
		return err
	}
	return nil
}

func (t tester) runTestSuites() error {
	var errStrs []string
	for _, suite := range t.testSuites {
		if err := suite.run(); err != nil {
			if t.exitOnFirstFailure {
				return err
			}
			errStrs = append(errStrs, suite.name+" - "+err.Error())
		}
	}

	if len(errStrs) != 0 {
		return fmt.Errorf("test suites errored: %v", errStrs)
	}
	return nil
}

type testSuite struct {
	name      string
	blueprint string
	test      string

	output      string
	passed      bool
	timeElapsed time.Duration

	// The path to write the JUnit result file.
	junitOutFile string
}

func (ts *testSuite) run() error {
	testStart := time.Now()
	l := log.testerLogger

	defer func() {
		ts.timeElapsed = time.Since(testStart)
		junitReport := newJUnitReport(*ts)
		writeJUnitReport(ts.junitOutFile, junitReport)
	}()
	defer func() {
		logsPath := filepath.Join(os.Getenv("WORKSPACE"), ts.name+"_debug_logs")
		cmd := exec.Command("kelda", "-v", "debug-logs", "-tar=false",
			"-o="+logsPath, "-all")
		stdout, stderr, err := execCmd(cmd, "DEBUG LOGS", log.cmdLogger)
		if err != nil {
			l.errorln(fmt.Sprintf("Debug logs encountered an error:"+
				" %v\nstdout: %s\nstderr: %s", err, stdout, stderr))
		}
	}()

	l.infoln(fmt.Sprintf("Test Suite: %s", ts.name))
	l.infoln("Start " + ts.name + ".js")
	contents, _ := fileContents(ts.blueprint)
	l.println(contents)
	l.infoln("End " + ts.name + ".js")
	defer l.infoln(fmt.Sprintf("Finished Test Suite: %s", ts.name))

	runBlueprint(ts.blueprint)

	bp, err := blueprint.FromFile(ts.blueprint)
	if err != nil {
		l.infoln(fmt.Sprintf("Error compiling blueprint: %s", err.Error()))
		return err
	}

	l.infoln("Waiting for containers to start up")
	if err = testerUtil.WaitForContainers(bp); err != nil {
		ts.output = "Containers never started: " + err.Error()
		l.println(".. " + ts.output)
		return err
	}

	if ts.test == "" {
		// If the test doesn't have any test binaries, then successfully
		// booting the blueprint is a "pass".
		l.println(".... Passed")
		ts.passed = true
	} else {
		// Wait a little bit longer for any container bootstrapping after boot.
		time.Sleep(90 * time.Second)

		l.infoln("Starting Test")
		l.println(".. " + filepath.Base(ts.test))

		ts.output, err = runTest(ts.test)
		if err == nil {
			l.println(".... Passed")
			ts.passed = true
		} else {
			l.println(".... Failed")
		}
	}

	return err
}

func runTest(testPath string) (string, error) {
	output, err := exec.Command(testPath).CombinedOutput()
	if err != nil {
		_, testName := filepath.Split(testPath)
		err = fmt.Errorf("test failed: %s", testName)
	}
	return string(output), err
}
