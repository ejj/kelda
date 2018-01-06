package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/minion/supervisor"

	"github.com/coreos/go-semver/semver"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/client"
	"github.com/docker/distribution/registry/client/auth"
	"github.com/docker/distribution/registry/client/transport"
	"github.com/docker/docker/registry"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/html"
)

const disableTestMessage = "Version tests are temporarily disabled until we " +
	"figure out a way to acknowledge failures while the versions are being updated"

// The names of the containers for which the version should be checked.
var systemContainerNames = []string{
	supervisor.EtcdName,
	supervisor.RegistryName,
}

func TestSystemContainerVersions(t *testing.T) {
	t.Skip(disableTestMessage)
	skipIfNotDev(t)

	aMaster, err := getMaster()
	assert.NoError(t, err)

	for _, name := range systemContainerNames {
		image, err := getImage(aMaster.CloudID, name)
		assert.NoError(t, err)

		imageRef, err := reference.ParseNormalizedNamed(image)
		assert.NoError(t, err)

		allTags, err := getAllTags(imageRef)
		assert.NoError(t, err)

		currTag := imageRef.(reference.NamedTagged).Tag()
		assertNewestVersion(t, name, currTag, allTags)
	}
}

func getImage(machineID, containerName string) (string, error) {
	stderr := bytes.NewBuffer(nil)
	cmd := exec.Command("kelda", "ssh", machineID, "docker", "inspect",
		containerName, "--format", "{{.Config.Image}}")
	cmd.Stderr = stderr

	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch image (%s): %s",
			err, stderr.String())
	}

	return strings.TrimRight(string(stdout), "\n"), nil
}

func getAllTags(image reference.Named) ([]string, error) {
	// Get an endpoint that we can query for the tag information.
	registryService, err := registry.NewService(registry.ServiceOptions{V2Only: true})
	if err != nil {
		return nil, err
	}

	repoInfo, err := registryService.ResolveRepository(image)
	if err != nil {
		return nil, err
	}

	endpoints, err := registryService.LookupPullEndpoints(
		reference.Domain(repoInfo.Name))
	if err != nil {
		return nil, err
	}

	if len(endpoints) == 0 {
		return nil, errors.New("no endpoints")
	}

	// Create a reference without the leading domain. For example,
	// quay.io/coreos/etcd becomes coreos/etcd.
	repoNameOnly, err := reference.WithName(reference.Path(repoInfo.Name))
	if err != nil {
		return nil, err
	}

	// Querying Docker registries requires authentication. We will authenticate
	// using tokens generated at runtime. Because we are only listing tags,
	// we don't need to provide any other secrets as a seed.
	challengeManager, _, err := registry.PingV2Registry(endpoints[0].URL,
		http.DefaultTransport)
	if err != nil {
		return nil, err
	}

	tokenHandlerOptions := auth.TokenHandlerOptions{
		Transport: http.DefaultTransport,
		Scopes: []auth.Scope{auth.RepositoryScope{
			Repository: repoNameOnly.Name(),
			Actions:    []string{"pull"},
			Class:      repoInfo.Class,
		}},
	}
	tokenHandler := auth.NewTokenHandlerWithOptions(tokenHandlerOptions)
	tr := transport.NewTransport(http.DefaultTransport,
		auth.NewAuthorizer(challengeManager, tokenHandler))

	repo, err := client.NewRepository(repoNameOnly, endpoints[0].URL.String(), tr)
	if err != nil {
		return nil, err
	}

	return repo.Tags(context.Background()).All(context.Background())
}

func TestDockerVersion(t *testing.T) {
	t.Skip(disableTestMessage)
	skipIfNotDev(t)

	aMaster, err := getMaster()
	assert.NoError(t, err)

	allVersions, err := parseReleaseLinks(
		"https://download.docker.com/linux/static/stable/x86_64/",
		regexp.MustCompile(`docker-(\d+\.\d+\.\d+-\w+).tgz`))
	assert.NoError(t, err)

	stdout, err := exec.Command("kelda", "ssh", aMaster.CloudID,
		"docker", "version", "--format", "{{.Server.Version}}").Output()
	assert.NoError(t, err)
	currVer := strings.TrimRight(string(stdout), "\n")
	assertNewestVersion(t, "docker", currVer, allVersions)
}

func TestOVSVersion(t *testing.T) {
	t.Skip(disableTestMessage)
	skipIfNotDev(t)

	aMaster, err := getMaster()
	assert.NoError(t, err)

	allVersions, err := parseReleaseLinks("http://openvswitch.org/download/",
		regexp.MustCompile(`openvswitch-(\d+\.\d+\.\d+).tar.gz`))
	assert.NoError(t, err)

	stdout, err := exec.Command("kelda", "ssh", aMaster.CloudID, "docker",
		"exec", supervisor.OvnnorthdName, "ovn-northd", "--version").Output()
	assert.NoError(t, err)

	// The output is in the form "ovn-northd (Open vSwitch) 2.8.1".
	stdoutParts := strings.Split(string(stdout), " ")
	currVer := strings.TrimRight(stdoutParts[len(stdoutParts)-1], "\n")
	assertNewestVersion(t, "ovs", currVer, allVersions)
}

// parseReleaseLinks gets the versions listed on a page by fetching the page,
// and applying the versionRegex to each link on the page. This is useful for
// parsing download pages that list releases.
func parseReleaseLinks(url string, versionRegex *regexp.Regexp) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, errors.New("unexpected status code")
	}

	var versions []string
	tokenizer := html.NewTokenizer(resp.Body)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			err = tokenizer.Err()
			if err == io.EOF {
				return versions, nil
			}
			return nil, err
		case html.StartTagToken:
			possibleLink := tokenizer.Token()
			if possibleLink.Data != "a" {
				continue
			}

			var link string
			for _, attr := range possibleLink.Attr {
				if attr.Key == "href" {
					link = attr.Val
					break
				}
			}

			versionMatches := versionRegex.FindAllStringSubmatch(link, 1)
			if len(versionMatches) != 0 {
				versions = append(versions, versionMatches[0][1])
			}
		}
	}
}

func getMaster() (db.Machine, error) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		return db.Machine{}, err
	}
	defer clnt.Close()

	machines, err := clnt.QueryMachines()
	if err != nil {
		return db.Machine{}, err
	}

	for _, m := range machines {
		if m.Role == db.Master {
			return m, nil
		}
	}
	return db.Machine{}, errors.New("no master machines")
}

// We only care whether we are running the latest version when running tests
// against the master branch of kelda/kelda since we won't update the versions
// on Kelda releases that have already been published.
func skipIfNotDev(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	assert.NoError(t, err)
	defer clnt.Close()

	version, err := clnt.Version()
	assert.NoError(t, err)
	if version != "dev" {
		t.Skip(`Skipping version check because Kelda version is not "dev"`)
	}
}

func assertNewestVersion(t *testing.T, name, currVersionStr string,
	allVersionStrs []string) {
	currVersion, err := semver.NewVersion(toSemanticVersion(currVersionStr))
	assert.NoError(t, err, "failed to parse current version for "+name)

	assert.NotEmpty(t, allVersionStrs, fmt.Sprintf("No versions supplied for %s. "+
		"The version generation code is probably broken.", name))
	var allVersions semver.Versions
	for _, verStr := range allVersionStrs {
		ver, err := semver.NewVersion(toSemanticVersion(verStr))
		if err != nil {
			fmt.Printf("Failed to parse version %s for %s: %s. Skipping.\n",
				verStr, name, err)
			continue
		}

		allVersions = append(allVersions, ver)
	}

	semver.Sort(allVersions)
	newestVersion := allVersions[len(allVersions)-1]
	if !currVersion.Equal(*newestVersion) {
		t.Errorf("%s (%s): Version %s is newer",
			name, currVersionStr, newestVersion)
	}
}

// toSemanticVersion attempts to convert the given string into the standard
// semantic version format. It removes the leading "v" if it's present, and
// appends 0's if necessary to fill in the minor and patch versions.
// For example, "v0.2" would become "0.2.0".
func toSemanticVersion(str string) string {
	if strings.HasPrefix(str, "v") {
		str = str[1:]
	}

	if numSeparators := strings.Count(str, "."); numSeparators < 2 {
		str += strings.Repeat(".0", 2-numSeparators)
	}

	return str
}
