package util

import (
	"fmt"

	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/db"
)

// GetContainer retrieves the container tracked by the given client with the
// given stitchID.
func GetContainer(c client.Client, stitchID string) (db.Container, error) {
	containers, err := c.QueryContainers()
	if err != nil {
		return db.Container{}, err
	}

	var choice *db.Container
	for _, c := range containers {
		if len(stitchID) > len(c.StitchID) ||
			c.StitchID[0:len(stitchID)] != stitchID {
			continue
		}

		if choice != nil {
			err := fmt.Errorf("ambiguous stitchIDs %s and %s",
				choice.StitchID, c.StitchID)
			return db.Container{}, err
		}

		copy := c
		choice = &copy
	}

	if choice != nil {
		return *choice, nil
	}

	return db.Container{}, fmt.Errorf("no container with stitchID %q", stitchID)
}
