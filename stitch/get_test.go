package stitch

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/quilt/quilt/util"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestGetQuiltPath(t *testing.T) {
	os.Setenv(QuiltPathKey, "")
	actual := GetQuiltPath()
	usr, err := user.Current()
	if err != nil {
		t.Error(err)
	}
	expected := filepath.Join(usr.HomeDir, ".quilt")
	if actual != expected {
		t.Errorf("expected %s \n but got %s", expected, actual)
	}
}

// repoLogger logs the directories interacted with for each repo
type repoLogger struct {
	created map[string][]string
	updated map[string][]string
}

func newRepoLogger() repoLogger {
	return repoLogger{
		created: make(map[string][]string),
		updated: make(map[string][]string),
	}
}

type mockRepo struct {
	repoName string
	toCreate []file

	logger *repoLogger
}

type file struct {
	name     string
	contents string
}

func (rl *repoLogger) newRepoFactory(toCreate map[string][]file) func(string) (
	repo, error) {

	return func(repoName string) (repo, error) {
		repo := mockRepo{
			repoName: repoName,
			logger:   rl,
		}
		if toCreate != nil {
			repo.toCreate = toCreate[repoName]
		}
		return &repo, nil
	}
}

func (mr *mockRepo) update(dir string) error {
	mr.logger.updated[mr.repoName] = append(mr.logger.updated[mr.repoName], dir)
	return nil
}

func (mr *mockRepo) create(dir string) error {
	for _, toCreate := range mr.toCreate {
		util.WriteFile(
			filepath.Join(dir, toCreate.name),
			[]byte(toCreate.contents),
			0644)
	}
	mr.logger.created[mr.repoName] = append(mr.logger.created[mr.repoName], dir)
	return nil
}

// The root is always the directory.
// e.g. github.com/quilt/quilt/specs/spark => github.com/quilt/quilt/specs,
// NOT github.com/quilt/quilt
func (mr *mockRepo) root() string {
	dir, _ := filepath.Split(mr.repoName)
	return dir
}

func TestImportExists(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	util.WriteFile("test.js", []byte(`require("existingImport")`), 0644)
	util.WriteFile("existingImport.js", []byte(""), 0644)

	logger := newRepoLogger()
	getter := ImportGetter{
		Path:        ".",
		repoFactory: logger.newRepoFactory(nil),
	}
	if err := getter.checkSpec("test.js", nil, nil); err != nil {
		t.Error(err)
		return
	}

	assert.Empty(t, logger.created, "Shouldn't create any repos")
	assert.Empty(t, logger.updated, "Shouldn't update any repos")
}

func TestAutoDownload(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	repoName := "autodownload"
	importPath := filepath.Join(repoName, "foo")
	util.WriteFile("test.js", []byte(fmt.Sprintf("require(%q);", importPath)), 0644)

	logger := newRepoLogger()
	getter := ImportGetter{
		Path:        ".",
		repoFactory: logger.newRepoFactory(nil),
	}

	expErr := "StitchError: unable to open import autodownload/foo: no loadable file"
	err := getter.checkSpec("test.js", nil, nil)
	if err == nil || err.Error() != expErr {
		t.Errorf("Wrong error, expected %q, got %v", expErr, err)
		return
	}

	assert.Equal(t, map[string][]string{
		importPath: {repoName},
	}, logger.created, "Should autodownload the repo")
	assert.Empty(t, logger.updated, "Shouldn't update any repos")
}

func TestGetCreate(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	quiltPath := "./getspecs"
	repoName := "github.com/quilt/quilt"
	importPath := filepath.Join(repoName, "foo")

	logger := newRepoLogger()
	getter := ImportGetter{
		Path:        quiltPath,
		repoFactory: logger.newRepoFactory(nil),
	}

	if err := getter.Get(importPath); err != nil {
		t.Error(err)
		return
	}

	assert.Equal(t, map[string][]string{
		importPath: {filepath.Join(quiltPath, repoName)},
	}, logger.created, "Should download the repo")
	assert.Empty(t, logger.updated, "Shouldn't update any repos")
}

func TestGetWalk(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	quiltPath := "getspecs"

	repoOne := "repoOne"
	importOne := filepath.Join(repoOne, "file")

	repoTwo := "anotherRepo"
	fileTwo := "importTwo"
	importTwo := filepath.Join(repoTwo, fileTwo)

	logger := newRepoLogger()
	getter := ImportGetter{
		Path: quiltPath,
		repoFactory: logger.newRepoFactory(map[string][]file{
			importOne: {
				{
					name:     "foo.js",
					contents: fmt.Sprintf("require(%q);", importTwo),
				},
			},
			importTwo: {
				{
					name: fmt.Sprintf("%s.js", fileTwo),
				},
			},
		}),
	}

	if err := getter.Get(importOne); err != nil {
		t.Error(err)
		return
	}

	assert.Equal(t, map[string][]string{
		importOne: {filepath.Join(quiltPath, repoOne)},
		importTwo: {filepath.Join(quiltPath, repoTwo)},
	}, logger.created, "Should autodownload the imported repo")
	assert.Empty(t, logger.updated, "Shouldn't update any repos")
}

func TestGetUpdate(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	quiltPath := "./getspecs"
	repoName := "github.com/quilt/quilt"
	util.AppFs.Mkdir(filepath.Join(quiltPath, repoName), 755)

	logger := newRepoLogger()
	getter := ImportGetter{
		Path:        quiltPath,
		repoFactory: logger.newRepoFactory(nil),
	}

	importPath := filepath.Join(repoName, "foo")
	if err := getter.Get(importPath); err != nil {
		t.Error(err)
		return
	}

	assert.Empty(t, logger.created, "Shouldn't create any repos")
	assert.Equal(t, map[string][]string{
		importPath: {filepath.Join(quiltPath, repoName)},
	}, logger.updated, "Should update the repo")
}

type requireTest struct {
	files     []file
	quiltPath string
	mainFile  string
	expVal    interface{}
	expErr    string
}

func TestRequire(t *testing.T) {
	squarer := `exports.square = function(x) {
		return x*x;
	};`
	tests := []requireTest{
		// Import returning a primitive.
		{
			files: []file{
				{
					name:     "/quilt_path/math.js",
					contents: squarer,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require("math").square(5);`,
			expVal:    float64(25),
		},
		// Import returning a type.
		{
			files: []file{
				{
					name: "/quilt_path/testImport.js",
					contents: `exports.getService = function() {
		return new Service("foo", []);
		};`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require("testImport").getService().hostname();`,
			expVal:    "foo.q",
		},
		// Import with an import.
		{
			files: []file{
				{
					name:     "/quilt_path/square.js",
					contents: squarer,
				},
				{
					name: "/quilt_path/cube.js",
					contents: `var square = require("square");
					exports.cube = function(x) {
						return x * square.square(x);
					};`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('cube').cube(5);`,
			expVal:    float64(125),
		},
		// Directly assigned exports.
		{
			files: []file{
				{
					name: "/quilt_path/square.js",
					contents: `module.exports = function(x) {
						return x*x
					}`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('square')(5);`,
			expVal:    float64(25),
		},
		// Test self import cycle.
		{
			files: []file{
				{
					name:     "/quilt_path/A.js",
					contents: `require("A");`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require("A");`,
			expErr:    `StitchError: import cycle: [A A]`,
		},
		// Test transitive import cycle.
		{
			files: []file{
				{
					name:     "/quilt_path/A.js",
					contents: `require("B");`,
				},
				{
					name:     "/quilt_path/B.js",
					contents: `require("A");`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('A');`,
			expErr:    `StitchError: import cycle: [A B A]`,
		},
		// No error if there's a path between two imports, but no cycle.
		{
			files: []file{
				{
					name:     "/quilt_path/A.js",
					contents: `require("B");`,
				},
				{
					name: "/quilt_path/B.js",
				},
			},
			quiltPath: "/quilt_path",
			mainFile: `require('B');
			require('A');
			"end"`,
			expVal: "end",
		},
		// Absolute import.
		{
			files: []file{
				{
					name:     "/abs/import.js",
					contents: squarer,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('/abs/import').square(5);`,
			expVal:    float64(25),
		},
		// Relative import.
		{
			files: []file{
				{
					name:     "/quilt_path/rel/square.js",
					contents: squarer,
				},
				{
					name: "/quilt_path/cube.js",
					contents: `var square = require("./rel/square");
					exports.cube = function(x) {
						return x * square.square(x);
					};`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('cube').cube(5);`,
			expVal:    float64(125),
		},
		// JSON import.
		{
			files: []file{
				{
					name: "/quilt_path/static.json",
					contents: `{
						"key": "val"
					}`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('static')['key'];`,
			expVal:    "val",
		},
		// JSON import of improperly formatted file.
		{
			files: []file{
				{
					name: "/quilt_path/static.json",
					contents: `{
						key: "val"
					}`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('static');`,
			expErr: "StitchError: unable to open import static: " +
				"invalid character 'k' looking for beginning of " +
				"object key string",
		},
		// Directory import with index.js.
		{
			files: []file{
				{
					name:     "/quilt_path/square/index.js",
					contents: squarer,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('square').square(5);`,
			expVal:    float64(25),
		},
		// Directory import with package.json.
		{
			files: []file{
				{
					name: "/quilt_path/square/package.json",
					contents: `{
						"main": "./foo.js"
					}
					`,
				},
				{
					name:     "/quilt_path/square/foo.js",
					contents: squarer,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('square').square(5);`,
			expVal:    float64(25),
		},
		// package.json formatting errors.
		{
			files: []file{
				{
					name: "/quilt_path/pkg-json/package.json",
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('pkg-json')`,
			expErr: "StitchError: unable to open import pkg-json: " +
				"unexpected end of JSON input",
		},
		{
			files: []file{
				{
					name:     "/quilt_path/pkg-json/package.json",
					contents: `{"main": 2}`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('pkg-json')`,
			expErr: "StitchError: unable to open import pkg-json: " +
				"bad package.json format",
		},
		// Missing files errors.
		{
			files: []file{
				{
					name:     "/quilt_path/pkg-json/package.json",
					contents: `{"main": "nonexistent"}`,
				},
			},
			quiltPath: "/quilt_path",
			mainFile:  `require('pkg-json')`,
			expErr: "StitchError: unable to open import pkg-json: " +
				"no loadable file",
		},
		{
			mainFile: `require('missing')`,
			expErr: "StitchError: unable to open import missing: " +
				"no loadable file",
		},
	}
	for _, test := range tests {
		util.AppFs = afero.NewMemMapFs()
		for _, f := range test.files {
			util.WriteFile(f.name, []byte(f.contents), 0644)
		}

		testVM, _ := newVM(ImportGetter{
			Path: test.quiltPath,
		})
		res, err := run(testVM, "main.js", test.mainFile)

		if err != nil || test.expErr != "" {
			assert.EqualError(t, err, test.expErr)
		} else {
			resIntf, _ := res.Export()
			assert.Equal(t, test.expVal, resIntf)
		}
	}
}
