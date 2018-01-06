package registry

import (
	"testing"

	dkc "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
)

func TestSyncImages(t *testing.T) {
	md, dk := docker.NewMock()
	conn := db.New()

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		im := view.InsertImage()
		im.Name = "image"
		view.Commit(im)

		m := view.InsertMinion()
		m.Self = true
		m.PrivateIP = "myIP"
		view.Commit(m)
		return nil
	})

	// Test building an image that fails. The status should not be "built".
	md.BuildError = true
	syncImages(conn, dk)
	images := getImages(conn)
	assert.Len(t, images, 1)
	assert.Empty(t, images[0].RepoDigest)
	assert.Empty(t, images[0].Status)

	// Test successfully building an image.
	md.BuildError = false
	syncImages(conn, dk)
	images = getImages(conn)
	assert.Len(t, images, 1)
	builtDigest := images[0].RepoDigest
	assert.NotEmpty(t, builtDigest, "should save repo digest of built image")
	assert.Equal(t, db.Built, images[0].Status)

	// Test ignoring already-built image.
	md.ResetBuilt()
	syncImages(conn, dk)

	images = getImages(conn)
	assert.Len(t, images, 1)
	assert.Equal(t, builtDigest, images[0].RepoDigest,
		"should not change repo digest")
	assert.Equal(t, md.Built, map[docker.BuildImageOptions]struct{}{},
		"should not attempt to rebuild")
}

func TestUpdateRegistry(t *testing.T) {
	md, dk := docker.NewMock()

	_, err := updateRegistry(dk, "myIP", db.Image{
		Name:       "mean:tag",
		Dockerfile: "dockerfile",
	})
	assert.NoError(t, err)

	assert.Equal(t, map[docker.BuildImageOptions]struct{}{
		{
			Name:       "myIP:5000/mean:tag",
			Dockerfile: "dockerfile",
			NoCache:    true,
		}: {},
	}, md.Built)

	assert.Equal(t, map[dkc.PushImageOptions]struct{}{
		{
			Registry: "myIP:5000",
			Name:     "myIP:5000/mean",
			Tag:      "tag",
		}: {},
	}, md.Pushed)
}

func TestGetImageHandle(t *testing.T) {
	t.Parallel()

	// The image that we'll be trying to retrieve.
	expImg := db.Image{Name: "foo", Dockerfile: "bar"}

	db.New().Txn(db.AllTables...).Run(func(view db.Database) error {
		// Test no matching images.
		im := view.InsertImage()
		im.Name = "other"
		im.Dockerfile = "ignoreme"
		view.Commit(im)

		_, err := getImageHandle(view, expImg)
		assert.NotNil(t, err)

		// Test one matching image.
		im = view.InsertImage()
		im.Name = expImg.Name
		im.Dockerfile = expImg.Dockerfile
		view.Commit(im)

		dbImg, err := getImageHandle(view, expImg)
		assert.NoError(t, err)
		assert.Equal(t, expImg.Name, dbImg.Name)
		assert.Equal(t, expImg.Dockerfile, dbImg.Dockerfile)
		assert.Equal(t, 2, dbImg.ID)

		// Test multiple matching images.
		im = view.InsertImage()
		im.Name = expImg.Name
		im.Dockerfile = expImg.Dockerfile
		view.Commit(im)

		_, err = getImageHandle(view, expImg)
		assert.NotNil(t, err)

		return nil
	})
}

func getImages(conn db.Conn) (images []db.Image) {
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		images = view.SelectFromImage(nil)
		return nil
	})
	return images
}
