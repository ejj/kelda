package kubernetes

import (
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/kubernetes/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateConfigMaps(t *testing.T) {
	t.Parallel()
	conn := db.New()
	client := &mocks.ConfigMapInterface{}

	fileMapA := map[string]blueprint.ContainerValue{
		"foo": blueprint.NewString("bar"),
		"qux": blueprint.NewString("quuz"),
	}
	configMapA := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "5775a40a17ef1905d6318251ecc6cd2c4122baa4",
		},
		Data: map[string]string{
			configMapKey("foo"): "bar",
			configMapKey("qux"): "quuz",
		},
	}

	fileMapB := map[string]blueprint.ContainerValue{
		"red": blueprint.NewString("blue"),
	}
	configMapB := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "f3bbe87650fce331a132e56501dde23101a1ccf7",
		},
		Data: map[string]string{
			configMapKey("red"): "blue",
		},
	}

	var fileMapAID int
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		dbc := view.InsertContainer()
		dbc.FilepathToContent = fileMapA
		fileMapAID = dbc.ID
		view.Commit(dbc)

		dbc = view.InsertContainer()
		dbc.FilepathToContent = fileMapB
		view.Commit(dbc)

		// A container with the same filepathToContent.
		dbc = view.InsertContainer()
		dbc.FilepathToContent = fileMapB
		view.Commit(dbc)

		// A container without a filepathToContent.
		dbc = view.InsertContainer()
		view.Commit(dbc)
		return nil
	})

	// At first, there are no existing config maps, so updateConfigMaps should
	// create both config maps.
	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{}, nil).Once()
	client.On("Create", &configMapA).Return(nil, nil).Once()
	client.On("Create", &configMapB).Return(nil, nil).Once()
	assert.True(t, updateConfigMaps(conn, client))
	client.AssertExpectations(t)

	// Then, a filepathToContent changes, so the old config map should be
	// removed, and a new one should be added.
	fileMapC := map[string]blueprint.ContainerValue{
		"red": blueprint.NewString("green"),
	}
	configMapC := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "35248adb3f7715582e013bfcc3fd8119be8fafc1",
		},
		Data: map[string]string{
			configMapKey("red"): "green",
		},
	}

	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		dbc := view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.ID == fileMapAID
		})[0]
		dbc.FilepathToContent = fileMapC
		view.Commit(dbc)
		return nil
	})

	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{
		Items: []corev1.ConfigMap{configMapA, configMapB},
	}, nil).Once()
	client.On("Create", &configMapC).Return(nil, nil).Once()
	client.On("Delete", configMapA.Name, mock.Anything).Return(nil).Once()

	assert.True(t, updateConfigMaps(conn, client))
	client.AssertExpectations(t)

	// If there are no changes to the filepathToContent maps, then no actions
	// need to be taken.
	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{
		Items: []corev1.ConfigMap{configMapC, configMapB},
	}, nil).Once()
	assert.True(t, updateConfigMaps(conn, client))

	// If all containers are removed, all config maps should be cleaned up.
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		for _, dbc := range view.SelectFromContainer(nil) {
			view.Remove(dbc)
		}
		return nil
	})
	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{
		Items: []corev1.ConfigMap{configMapC, configMapB},
	}, nil).Once()
	client.On("Delete", configMapB.Name, mock.Anything).Return(nil).Once()
	client.On("Delete", configMapC.Name, mock.Anything).Return(nil).Once()
	assert.True(t, updateConfigMaps(conn, client))
}

func TestUpdateConfigMapsErrors(t *testing.T) {
	t.Parallel()
	conn := db.New()
	client := &mocks.ConfigMapInterface{}

	// Test where we can't list the current config maps.
	client.On("List", mock.Anything).Return(nil, assert.AnError).Once()
	assert.False(t, updateConfigMaps(conn, client))
	client.AssertExpectations(t)

	// Test where deleting fails.
	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{
		Items: []corev1.ConfigMap{{}},
	}, nil).Once()
	client.On("Delete", mock.Anything, mock.Anything).Return(assert.AnError).Once()
	assert.False(t, updateConfigMaps(conn, client))
	client.AssertExpectations(t)

	// Test where creating fails.
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		dbc := view.InsertContainer()
		dbc.FilepathToContent = map[string]blueprint.ContainerValue{
			"foo": blueprint.NewString("bar"),
		}
		view.Commit(dbc)
		return nil
	})
	client.On("List", mock.Anything).Return(&corev1.ConfigMapList{}, nil).Once()
	client.On("Create", mock.Anything).Return(nil, assert.AnError).Once()
	assert.False(t, updateConfigMaps(conn, client))
	client.AssertExpectations(t)
}
