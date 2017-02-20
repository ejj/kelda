package command

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	log "github.com/Sirupsen/logrus"
	logrusTestHook "github.com/Sirupsen/logrus/hooks/test"
	"github.com/fatih/color"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	clientMock "github.com/quilt/quilt/api/client/mocks"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/stitch"
	"github.com/quilt/quilt/util"
)

type file struct {
	path, contents string
}

type runTest struct {
	files        []file
	path         string
	expExitCode  int
	expDeployArg string
	expEntries   []log.Entry
}

func TestRunSpec(t *testing.T) {
	os.Setenv("QUILT_PATH", "/quilt_path")
	stitch.DefaultImportGetter.Path = "/quilt_path"

	exJavascript := `deployment.deploy(new Machine({}));`
	exJSON := `{"Machines":[{"ID":"107dee4e67d9a0fead1ef7ac48adc0a5aebbedac",` +
		`"CPU":{},"RAM":{}}],"Namespace":"default-namespace"}`
	tests := []runTest{
		{
			files: []file{
				{
					path:     "test.js",
					contents: exJavascript,
				},
			},
			path:         "test.js",
			expExitCode:  0,
			expDeployArg: exJSON,
		},
		{
			path:        "dne.js",
			expExitCode: 1,
			expEntries: []log.Entry{
				{
					Message: "open /quilt_path/dne.js: " +
						"file does not exist",
					Level: log.ErrorLevel,
				},
			},
		},
		{
			path:        "/dne.js",
			expExitCode: 1,
			expEntries: []log.Entry{
				{
					Message: "open /dne.js: file does not exist",
					Level:   log.ErrorLevel,
				},
			},
		},
		{
			files: []file{
				{
					path:     "/quilt_path/in_quilt_path.js",
					contents: exJavascript,
				},
			},
			path:         "in_quilt_path",
			expDeployArg: exJSON,
		},
		// Ensure we print a stacktrace when available.
		{
			files: []file{
				{
					path:     "/quilt_path/A.js",
					contents: `require("B").foo();`,
				},
				{
					path: "/quilt_path/B.js",
					contents: `module.exports.foo = function() {
						throw new Error("bar");
					}`,
				},
			},
			path:        "/quilt_path/A.js",
			expExitCode: 1,
			expEntries: []log.Entry{
				{
					Message: "Error: bar\n" +
						"    at /quilt_path/B.js:2:17\n" +
						"    at /quilt_path/A.js:1:67\n",
					Level: log.ErrorLevel,
				},
			},
		},
	}
	for _, test := range tests {
		util.AppFs = afero.NewMemMapFs()

		mockGetter := new(clientMock.Getter)
		c := &clientMock.Client{}
		mockGetter.On("Client", mock.Anything).Return(c, nil)

		logHook := logrusTestHook.NewGlobal()

		for _, f := range test.files {
			util.WriteFile(f.path, []byte(f.contents), 0644)
		}
		runCmd := NewRunCommand()
		runCmd.clientGetter = mockGetter
		runCmd.stitch = test.path
		exitCode := runCmd.Run()

		assert.Equal(t, test.expExitCode, exitCode)
		assert.Equal(t, test.expDeployArg, c.DeployArg)

		assert.Equal(t, len(test.expEntries), len(logHook.Entries))
		for i, entry := range logHook.Entries {
			assert.Equal(t, test.expEntries[i].Message, entry.Message)
			assert.Equal(t, test.expEntries[i].Level, entry.Level)
		}
	}
}

type diffTest struct {
	curr, new, exp string
}

func TestDeploymentDiff(t *testing.T) {
	t.Parallel()

	tests := []diffTest{
		{
			curr: "{}",
			new:  "{}",
			exp:  "",
		},
		{
			curr: `{"Machines":[{"Provider":"Amazon"}]}`,
			new:  `{"Machines":[]}`,
			exp: `--- Current
+++ Proposed
@@ -1,7 +1,3 @@
 {
-	"Machines": [
-		{
-			"Provider": "Amazon"
-		}
-	]
+	"Machines": []
 }
`,
		},
		{
			curr: `{"Machines":[{"Provider":"Amazon"},` +
				`{"Provider":"Google"}]}`,
			new: `{"Machines":[{"Provider":"Google"}]}`,
			exp: `--- Current
+++ Proposed
@@ -1,8 +1,5 @@
 {
 	"Machines": [
-		{
-			"Provider": "Amazon"
-		},
 		{
 			"Provider": "Google"
 		}
`,
		},
		{
			curr: `{"Machines":[{"Provider":"Amazon"},` +
				`{"Provider":"Google"}]}`,
			new: `{"Machines":[{"Provider":"Vagrant"}]}`,
			exp: `--- Current
+++ Proposed
@@ -1,10 +1,7 @@
 {
 	"Machines": [
 		{
-			"Provider": "Amazon"
-		},
-		{
-			"Provider": "Google"
+			"Provider": "Vagrant"
 		}
 	]
 }
`,
		},
	}

	for _, test := range tests {
		diff, err := diffDeployment(test.curr, test.new)
		assert.Nil(t, err)
		assert.Equal(t, test.exp, diff)
	}
}

type colorizeTest struct {
	original string
	exp      string
}

func TestColorize(t *testing.T) {
	green := "\x1b[32m"
	red := "\x1b[31m"
	// a reset sequence is inserted after a colorized line
	reset := "\x1b[0m"
	// force colored output for testing
	color.NoColor = false
	tests := []colorizeTest{
		{
			original: "{}",
			exp:      "{}",
		},
		{
			original: "no color\n" +
				"-\tred\n" +
				"+\tgreen\n",
			exp: "no color\n" +
				red + "-\tred\n" + reset +
				green + "+\tgreen\n" + reset,
		},
		{
			original: "\n",
			exp:      "\n",
		},
		{
			original: "\na\n\n",
			exp:      "\na\n\n",
		},
		{
			original: "+----+---+\n",
			exp:      green + "+----+---+\n" + reset,
		},
	}
	for _, test := range tests {
		colorized := colorizeDiff(test.original)
		assert.Equal(t, test.exp, colorized)
	}
}

type confirmTest struct {
	inputs []string
	exp    bool
}

func TestConfirm(t *testing.T) {
	tests := []confirmTest{
		{
			inputs: []string{"y"},
			exp:    true,
		},
		{
			inputs: []string{"yes"},
			exp:    true,
		},
		{
			inputs: []string{"YES"},
			exp:    true,
		},
		{
			inputs: []string{"n"},
			exp:    false,
		},
		{
			inputs: []string{"no"},
			exp:    false,
		},
		{
			inputs: []string{"foo", "no"},
			exp:    false,
		},
		{
			inputs: []string{"foo", "no", "yes"},
			exp:    false,
		},
	}
	for _, test := range tests {
		in := bytes.NewBufferString(strings.Join(test.inputs, "\n"))
		res, err := confirm(in, "")
		assert.Nil(t, err)
		assert.Equal(t, test.exp, res)
	}
}

func TestPromptsUser(t *testing.T) {
	oldConfirm := confirm
	defer func() {
		confirm = oldConfirm
	}()

	util.AppFs = afero.NewMemMapFs()
	for _, confirmResp := range []bool{true, false} {
		confirm = func(in io.Reader, prompt string) (bool, error) {
			return confirmResp, nil
		}

		mockGetter := new(clientMock.Getter)
		c := &clientMock.Client{
			ClusterReturn: []db.Cluster{
				{
					Spec: `{"old":"spec"}`,
				},
			},
		}
		mockGetter.On("Client", mock.Anything).Return(c, nil)

		util.WriteFile("test.js", []byte(""), 0644)
		runCmd := NewRunCommand()
		runCmd.clientGetter = mockGetter
		runCmd.stitch = "test.js"
		runCmd.Run()
		assert.Equal(t, confirmResp, c.DeployArg != "")
	}
}

func TestRunFlags(t *testing.T) {
	t.Parallel()

	expStitch := "spec"
	checkRunParsing(t, []string{"-stitch", expStitch}, Run{stitch: expStitch}, nil)
	checkRunParsing(t, []string{expStitch}, Run{stitch: expStitch}, nil)
	checkRunParsing(t, []string{"-f", expStitch},
		Run{force: true, stitch: expStitch}, nil)
	checkRunParsing(t, []string{}, Run{}, errors.New("no spec specified"))
}

func checkRunParsing(t *testing.T, args []string, expFlags Run, expErr error) {
	runCmd := NewRunCommand()
	err := parseHelper(runCmd, args)

	if expErr != nil {
		if err.Error() != expErr.Error() {
			t.Errorf("Expected error %s, but got %s",
				expErr.Error(), err.Error())
		}
		return
	}

	assert.Nil(t, err)
	assert.Equal(t, expFlags.stitch, runCmd.stitch)
	assert.Equal(t, expFlags.force, runCmd.force)
}
