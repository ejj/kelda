package registry

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"sync"
)

/*
The registry submodule builds custom Dockerfiles. When a custom Dockerfile is
deployed in a blueprint (e.g.`new Container({ name: 'name', image:
new Image({ name: 'name', dockerfile: 'dk' }) })`), a couple things happen:
1) On the leader, the engine reads the custom images from the Containers in the
blueprint, and writes them to the Image table.
2) The registry submodule builds the images in the Image table, and updates
their image ID with the ID of the built image.
3) The scheduler schedules containers for which the image has been built.
When scheduling Containers with custom images, it modifies the image to
be pointed at the registry running on the leader. A side effect of this is that
if the leader dies, the scheduler updates the image names in etcd, and the workers
restart containers running the custom image.
4) The workers pull and run the image just like any other image.
*/

// Run builds Docker images according to the Image table if the minion's Role is
// Master, and does nothing otherwise.
func Run(conn db.Conn, dk docker.Client) {
	if conn.MinionSelf().Role != db.Master {
		return
	}

	bootWait()
	for range conn.TriggerTick(30, db.ImageTable).C {
		syncImages(conn, dk)
	}
}

// syncImages checks the Image table for any images that have not yet been
// built, and builds them.
func syncImages(conn db.Conn, dk docker.Client) {
	var toBuild []db.Image
	conn.Txn(db.ImageTable).Run(func(view db.Database) error {
		toBuild = view.SelectFromImage(func(img db.Image) bool {
			return img.Status != db.Built
		})
		return nil
	})

	var wg sync.WaitGroup
	wg.Add(len(toBuild))
	sema := make(chan struct{}, 8)

	myIP := conn.MinionSelf().PrivateIP
	builder := func(img db.Image) {
		sema <- struct{}{}
		defer func() {
			writeImage(conn, img)
			<-sema
			wg.Done()
		}()

		img.Status = db.Building
		writeImage(conn, img)

		log.WithField("image", img.Name).Info("Building image...")
		repoDigest, err := updateRegistry(dk, myIP, img)
		if err != nil {
			img.Status = "" // Unset the building status.

			log.WithError(err).WithField("image", img.Name).
				Error("Failed to update registry")
			return
		}

		img.RepoDigest = repoDigest
		img.Status = db.Built

		log.WithField("image", img.Name).Info("Built image.")
	}

	for _, img := range toBuild {
		go builder(img)
	}
	wg.Wait()
}

func updateRegistry(dk docker.Client, registryIP string, img db.Image) (string, error) {
	registryImg := fmt.Sprintf("%s:5000/%s", registryIP, img.Name)
	err := dk.Build(registryImg, img.Dockerfile, false)
	if err != nil {
		return "", err
	}
	return dk.Push(registryIP+":5000", registryImg)
}

// writeImage updates the attributes of the image committed to the database that
// has the same Name and Dockerfile.
func writeImage(conn db.Conn, img db.Image) {
	err := conn.Txn(db.ImageTable).Run(
		func(view db.Database) error {
			dbImg, err := getImageHandle(view, img)
			if err == nil {
				img.ID = dbImg.ID
				view.Commit(img)
			}
			return err
		},
	)
	if err != nil {
		log.WithError(err).WithField("image", img).Warn("Failed to write image")
	}
}

func getImageHandle(view db.Database, ref db.Image) (db.Image, error) {
	matchingImages := view.SelectFromImage(func(img db.Image) bool {
		return img.Dockerfile == ref.Dockerfile && img.Name == ref.Name
	})
	switch len(matchingImages) {
	case 0:
		return db.Image{}, errors.New("no matching images")
	case 1:
		return matchingImages[0], nil
	default:
		return db.Image{}, errors.New("multiple matching images")
	}
}

// bootWait blocks until the registry is ready to be pushed to.
func bootWait() {
	for {
		_, err := http.Get("http://localhost:5000")
		if err != nil {
			log.WithError(err).Debug("Registry not up yet")
		} else {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
}
