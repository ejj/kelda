package stitch

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/tools/go/vcs"
	"os"
	"path/filepath"
	"strings"

	"github.com/quilt/quilt/util"

	log "github.com/Sirupsen/logrus"
	homedir "github.com/mitchellh/go-homedir"

	"github.com/robertkrimen/otto"
	"github.com/spf13/afero"
)

// QuiltPathKey is the environment variable key we use to lookup the Quilt path.
const QuiltPathKey = "QUILT_PATH"

// GetQuiltPath returns the user-defined QUILT_PATH, or the default absolute QUILT_PATH,
// which is ~/.quilt if the user did not specify a QUILT_PATH.
func GetQuiltPath() string {
	if quiltPath := os.Getenv(QuiltPathKey); quiltPath != "" {
		return quiltPath
	}

	dir, err := homedir.Dir()
	if err != nil {
		log.WithError(err).Fatalf("Failed to get user's homedir for "+
			"%s generation", QuiltPathKey)
	}

	return filepath.Join(dir, ".quilt")
}

// ImportGetter provides functions for working with imports.
type ImportGetter struct {
	Path         string
	AutoDownload bool

	repoFactory func(repo string) (repo, error)

	// Used to detect import cycles.
	importPath []string
}

func (getter ImportGetter) withAutoDownload(autoDownload bool) ImportGetter {
	return ImportGetter{
		Path:         getter.Path,
		AutoDownload: autoDownload,
		repoFactory:  getter.repoFactory,
	}
}

type repo interface {
	// Pull the latest changes in the repo to `dir`.
	update(dir string) error

	// Checkout the repo to `dir`.
	create(dir string) error

	// Get the root of the repo.
	root() string
}

// `goRepo` is a wrapper around `vcs.RepoRoot` that satisfies the `repo` interface.
type goRepo struct {
	repo *vcs.RepoRoot
}

func (gr goRepo) update(dir string) error {
	return gr.repo.VCS.Download(dir)
}

func (gr goRepo) create(dir string) error {
	return gr.repo.VCS.Create(dir, gr.repo.Repo)
}

func (gr goRepo) root() string {
	return gr.repo.Root
}

func goRepoFactory(repoName string) (repo, error) {
	vcsRepo, err := vcs.RepoRootForImportPath(repoName, true)
	return goRepo{vcsRepo}, err
}

// DefaultImportGetter uses the default QUILT_PATH, and doesn't automatically
// download imports.
var DefaultImportGetter = ImportGetter{
	Path:        GetQuiltPath(),
	repoFactory: goRepoFactory,
}

// Get takes in an import path `repoName`, and attempts to download the
// repository associated with that repoName.
func (getter ImportGetter) Get(repoName string) error {
	path, err := getter.downloadSpec(repoName)
	if err != nil {
		return err
	}
	return getter.resolveSpecImports(path)
}

func (getter ImportGetter) downloadSpec(repoName string) (string, error) {
	repo, err := getter.repoFactory(repoName)
	if err != nil {
		return "", err
	}

	path := filepath.Join(getter.Path, repo.root())
	if _, statErr := util.AppFs.Stat(path); os.IsNotExist(statErr) {
		log.Info(fmt.Sprintf("Cloning %s into %s", repo.root(), path))
		err = repo.create(path)
	} else {
		log.Info(fmt.Sprintf("Updating %s in %s", repo.root(), path))
		err = repo.update(path)
	}
	return path, err
}

func (getter ImportGetter) resolveSpecImports(folder string) error {
	return afero.Walk(util.AppFs, folder, getter.checkSpec)
}

func (getter ImportGetter) checkSpec(file string, _ os.FileInfo, _ error) error {
	if filepath.Ext(file) != ".js" {
		return nil
	}
	_, err := FromFile(file, getter.withAutoDownload(true))
	return err
}

// Error thrown when there are no files module files that can be read from disk.
var errNoLoadableFile = errors.New("no loadable file")

// loadAsFile searches for and evaluates `imp`, `imp`.js`, and finally `imp`.json.
// Once a loadable import file is found, it stops searching.
func loadAsFile(vm *otto.Otto, imp string) (otto.Value, error) {
	for _, suffix := range []string{"", ".js"} {
		if path := imp + suffix; isFile(path) {
			spec, err := util.ReadFile(path)
			if err != nil {
				return otto.Value{}, err
			}
			return runSpec(vm, path, spec)
		}
	}

	if path := imp + ".json"; isFile(path) {
		unmarshalled, err := unmarshalFile(path)
		if err != nil {
			return otto.Value{}, err
		}
		return vm.ToValue(unmarshalled)
	}

	return otto.Value{}, errNoLoadableFile
}

// loadAsDir searches for and evaluates `dir`/package.json. If `package.json`
// doesn't exist, it tries to load the import `dir`/index by following the file
// loading rules.
// Once a loadable import file is found, it stops searching.
func loadAsDir(vm *otto.Otto, dir string) (otto.Value, error) {
	if path := filepath.Join(dir, "package.json"); isFile(path) {
		intf, err := unmarshalFile(path)
		if err != nil {
			return otto.Value{}, err
		}

		pkg, ok := intf.(map[string]interface{})
		mainIntf, ok2 := pkg["main"]
		main, ok3 := mainIntf.(string)
		if !ok || !ok2 || !ok3 {
			return otto.Value{}, errors.New("bad package.json format")
		}
		return loadAsFile(vm, filepath.Join(dir, main))
	}

	return loadAsFile(vm, filepath.Join(dir, "index"))
}

func tryImport(vm *otto.Otto, path string) (otto.Value, error) {
	if imp, err := loadAsFile(vm, path); err != errNoLoadableFile {
		return imp, err
	}
	return loadAsDir(vm, path)
}

func (getter ImportGetter) resolveImportHelper(vm *otto.Otto, callerDir, name string) (
	imp otto.Value, err error) {

	switch {
	case isRelative(name):
		imp, err = tryImport(vm, filepath.Join(callerDir, name))
	case filepath.IsAbs(name):
		imp, err = tryImport(vm, name)
	default:
		imp, err = tryImport(vm, filepath.Join(getter.Path, name))
	}
	return imp, err
}

func (getter ImportGetter) resolveImport(vm *otto.Otto, callerDir, name string) (
	imp otto.Value, err error) {

	imp, err = getter.resolveImportHelper(vm, callerDir, name)
	// Autodownload if the import doesn't exist, and it's not a filesystem import.
	if err == errNoLoadableFile && !isRelative(name) && !filepath.IsAbs(name) &&
		getter.AutoDownload {
		getter.Get(name)
		imp, err = getter.resolveImportHelper(vm, callerDir, name)
	}
	switch err.(type) {
	case nil:
		return imp, nil
	// Don't munge the error if it's an evaluation error, and not a loading error.
	case *otto.Error:
		return otto.Value{}, err
	default:
		return otto.Value{}, fmt.Errorf("unable to open import %s: %s",
			name, err.Error())
	}
}

func (getter *ImportGetter) requireImpl(call otto.FunctionCall) (otto.Value, error) {
	if len(call.ArgumentList) != 1 {
		return otto.Value{}, errors.New(
			"require requires the import as an argument")
	}
	name, err := call.Argument(0).ToString()
	if err != nil {
		return otto.Value{}, err
	}

	// An import cycle exists if a spec imports one of its parents.
	// We detect this by keeping track of the path to get to the current import.
	// This slice is maintained by adding imports to the path when they're
	// initially imported, and removing them when all their children have finished
	// importing.
	if contains(getter.importPath, name) {
		return otto.Value{},
			fmt.Errorf("import cycle: %v", append(getter.importPath, name))
	}

	getter.importPath = append(getter.importPath, name)
	defer func() {
		getter.importPath = getter.importPath[:len(getter.importPath)-1]
	}()

	callerDir := filepath.Dir(call.Otto.Context().Filename)
	return getter.resolveImport(call.Otto, callerDir, name)
}

func isFile(path string) bool {
	info, err := util.AppFs.Stat(path)
	return err == nil && !info.IsDir()
}

func isRelative(path string) bool {
	return strings.HasPrefix(path, ".") || strings.HasPrefix(path, "..")
}

func unmarshalFile(path string) (parsed interface{}, err error) {
	contents, err := util.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal([]byte(contents), &parsed)
	return parsed, err
}
