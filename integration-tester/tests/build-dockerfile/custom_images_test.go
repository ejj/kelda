package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
	"time"
)

func TestCustomImages(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	// The images deployed for the given Dockerfile.
	dockerfileToImages := make(map[string][]string)

	// The number of containers deployed for each Dockerfile.
	dockerfileCount := make(map[string]int)

	for _, c := range containers {
		if !strings.Contains(c.Image, "test-custom-image") {
			continue
		}

		dockerfileID, imageID, err := getContainerInfo(c.BlueprintID)
		if err != nil {
			t.Fatalf("couldn't get container info for %s: %s",
				c.BlueprintID, err)
		}

		dockerfileToImages[dockerfileID] = append(
			dockerfileToImages[dockerfileID], imageID)
		dockerfileCount[dockerfileID]++
	}

	fmt.Println("Dockerfile to image mappings:", dockerfileToImages)
	fmt.Println("Dockerfile counts:", dockerfileCount)

	reuseErr := checkReuseImage(dockerfileToImages)
	if reuseErr != nil {
		t.Error(reuseErr)
	}

	countErr := checkImageCounts(machines, dockerfileCount)
	if countErr != nil {
		t.Error(countErr)
	}

	parallelErr := checkBuildParallelized(machines, containers)
	if parallelErr != nil {
		t.Error(parallelErr)
	}
}

func checkReuseImage(dockerfileToImages map[string][]string) error {
	for dk, images := range dockerfileToImages {
		for _, otherImg := range images {
			if otherImg != images[0] {
				return fmt.Errorf("images for DockerfileID %s not "+
					"reused: %v", dk, images)
			}
		}
	}
	return nil
}

func checkImageCounts(machines []db.Machine, dockerfileCounts map[string]int) error {
	nWorker := 0
	for _, m := range machines {
		if m.Role == db.Worker {
			nWorker++
		}
	}

	for i := 0; i < len(dockerfileCounts); i++ {
		if actual := dockerfileCounts[strconv.Itoa(i)]; actual != nWorker {
			return fmt.Errorf("DockerfileID %d had %d containers, "+
				"expected %d", i, actual, nWorker)
		}
	}

	return nil
}

func checkBuildParallelized(machines []db.Machine, containers []db.Container) error {
	firstContainer := make(map[string]time.Time)
	lastContainer := make(map[string]time.Time)

	for _, c := range containers {
		if t, ok := firstContainer[c.Minion]; !ok || c.Created.Before(t) {
			firstContainer[c.Minion] = c.Created
		}
		if t, ok := lastContainer[c.Minion]; !ok || c.Created.After(t) {
			lastContainer[c.Minion] = c.Created
		}
	}

	for _, m := range machines {
		first, last := firstContainer[m.PrivateIP], lastContainer[m.PrivateIP]
		duration := last.Sub(first)
		maxDuration := 15 * time.Second
		if duration > maxDuration {
			return fmt.Errorf("machine %s has containers that started %d ms"+
				" apart, expected <%d", m.CloudID, duration, maxDuration)
		}
	}

	return nil
}

func getContainerInfo(blueprintID string) (string, string, error) {
	dockerfileID, err := exec.Command(
		"kelda", "ssh", blueprintID, "cat /dockerfile-id").CombinedOutput()
	if err != nil {
		return "", "", err
	}
	imageID, err := exec.Command(
		"kelda", "ssh", blueprintID, "cat /image-id").CombinedOutput()
	return strings.TrimSpace(string(dockerfileID)),
		strings.TrimSpace(string(imageID)),
		err
}
