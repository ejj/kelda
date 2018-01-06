package kubernetes

import (
	"crypto/sha1"
	"fmt"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// updateConfigMaps creates a config map for each container's
// FilepathToContent. It ignores any Secret ContainerValues.
// These config maps are referenced by the updateDeployment function to mount
// the container's filepathToContent into the container.
func updateConfigMaps(conn db.Conn, configMapsClient clientv1.ConfigMapInterface) (
	noErrors bool) {

	noErrors = true
	currentConfigMaps, err := configMapsClient.List(metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to list current config maps")
		return false
	}

	key := func(intf interface{}) interface{} {
		return intf.(corev1.ConfigMap).Name
	}
	_, toCreate, toDelete := join.HashJoin(
		configMapSlice(getDesiredConfigMaps(conn)),
		configMapSlice(currentConfigMaps.Items),
		key, key)

	for _, intf := range toCreate {
		configMap := intf.(corev1.ConfigMap)
		c.Inc("Create ConfigMap")
		if _, err := configMapsClient.Create(&configMap); err != nil {
			log.WithError(err).WithField("configMap", configMap.Name).
				Error("Failed to create config map")
			noErrors = false
		}
	}

	for _, intf := range toDelete {
		configMap := intf.(corev1.ConfigMap)
		c.Inc("Delete ConfigMap")
		err := configMapsClient.Delete(configMap.Name, &metav1.DeleteOptions{})
		if err != nil {
			log.WithError(err).WithField("configMap", configMap.Name).
				Error("Failed to delete config map")
			noErrors = false
		}
	}
	return noErrors
}

// getDesiredConfigMaps creates a config map for each unique container
// filepathToContent. The name of the config map is a unique and consistent
// identifier based on the contents of the map. See the documentation for
// configMapName for more details.
func getDesiredConfigMaps(conn db.Conn) (configMaps []corev1.ConfigMap) {
	// Filter out duplicate configMaps to avoid attempting to create two
	// configMaps with the same name.
	hashes := map[string]struct{}{}
	for _, dbc := range conn.SelectFromContainer(nil) {
		if len(dbc.FilepathToContent) == 0 {
			continue
		}

		rawStrings, _ := blueprint.DivideContainerValues(dbc.FilepathToContent)
		if len(rawStrings) == 0 {
			continue
		}

		config := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: configMapName(rawStrings),
			},
			Data: map[string]string{},
		}
		for k, v := range rawStrings {
			config.Data[configMapKey(k)] = v
		}

		if _, ok := hashes[config.Name]; !ok {
			configMaps = append(configMaps, config)
			hashes[config.Name] = struct{}{}
		}
	}
	return configMaps
}

// name returns a consistent hash representing the contents of the fileMap.
// This serves as a signal to pods that the fileMap contents have changed, and
// thus the pod needs to be restarted. It is also useful as a stateless way to
// coordinate the ConfigMap name between the updateConfigMaps and
// updateDeployments goroutines.
func configMapName(fm map[string]string) string {
	toHash := str.MapAsString(fm)
	return fmt.Sprintf("%x", sha1.Sum([]byte(toHash)))
}

// ConfigMap keys must be lowercase alphanumeric characters.
func configMapKey(path string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(path)))
}

type configMapSlice []corev1.ConfigMap

func (slc configMapSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc configMapSlice) Len() int {
	return len(slc)
}
