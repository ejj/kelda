package scheduler

import (
	"crypto/sha1"
	"fmt"
	"sort"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const filesVolumeName = "filepath-to-content"

func updateConfigMaps(conn db.Conn, configMapsClient clientv1.ConfigMapInterface) (noErrors bool) {
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
		configMapSlice(makeDesiredConfigMaps(conn)),
		configMapSlice(currentConfigMaps.Items),
		key, key)

	for _, intf := range toCreate {
		configMap := intf.(corev1.ConfigMap)
		if _, err := configMapsClient.Create(&configMap); err != nil {
			log.WithError(err).WithField("configMaps", configMap.Name).
				Error("Failed to create config map")
			noErrors = false
		}
	}

	for _, intf := range toDelete {
		configMap := intf.(corev1.ConfigMap)
		err := configMapsClient.Delete(configMap.Name, &metav1.DeleteOptions{})
		if err != nil {
			log.WithError(err).WithField("configMap", configMap.Name).
				Error("Failed to delete config map")
			noErrors = false
		}
	}
	return noErrors
}

func makeDesiredConfigMaps(conn db.Conn) (configMaps []corev1.ConfigMap) {
	// Filter out duplicate configMaps to avoid attempting to create two
	// configMaps with the same name.
	hashes := map[string]struct{}{}
	for _, dbc := range conn.SelectFromContainer(nil) {
		if len(dbc.FilepathToContent) == 0 {
			continue
		}

		config := newFileMap(dbc.FilepathToContent).configMap()
		if _, ok := hashes[config.Name]; !ok {
			configMaps = append(configMaps, config)
			hashes[config.Name] = struct{}{}
		}
	}
	return configMaps
}

type fileMap map[string]string

func newFileMap(valMap map[string]blueprint.ContainerValue) fileMap {
	fm := fileMap{}
	for k, v := range valMap {
		if valStr, ok := v.Value.(string); ok {
			fm[k] = valStr
		}
	}
	return fm
}

// name returns a consistent hash representing the contents of the fileMap.
// This serves as a signal to pods that the fileMap contents have changed, and
// thus the pod needs to be restarted. It is also useful as a stateless way to
// coordinate the ConfigMap name between the updateConfigMaps and
// updateDeployments goroutines.
func (fm fileMap) configMapName() string {
	toHash := str.MapAsString(fm)
	return fmt.Sprintf("%x", sha1.Sum([]byte(toHash)))
}

// configMap returns the ConfigMap representing the fileMap data so that it can
// be mounted by volumes and be made visible to containers.
func (fm fileMap) configMap() corev1.ConfigMap {
	data := map[string]string{}
	for k, v := range fm {
		data[pathToKey(k)] = v
	}
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: fm.configMapName(),
		},
		Data: data,
	}
}

// volume returns the Volume that should be attached to a pod in order to
// reference the configMap containing the fileMap. The configMap returned by
// fileMap.configMap() must have been synced to the Kubernetes cluster.
func (fm fileMap) volume() corev1.Volume {
	mode := int32(0444)
	return corev1.Volume{
		Name: filesVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: fm.configMapName(),
				},
				DefaultMode: &mode,
			},
		},
	}
}

// volumeMounts returns the VolumeMounts that should be attached to a container
// in order to mount the files in the fileMap at the correct paths. The
// VolumeMounts reference the Volume returned by fileMap.volume() -- that
// volume must be defined in the container's pod.
func (fm fileMap) volumeMounts() (mounts []corev1.VolumeMount) {
	for path := range fm {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      filesVolumeName,
			ReadOnly:  true,
			MountPath: path,
			SubPath:   pathToKey(path),
		})
	}
	// Sort the volume mounts so that the pod config is consistent. Otherwise,
	// Kubernetes will treat the difference in VolumeMount orderings as a
	// reason to restart the pod.
	sort.Sort(volumeMountSlice(mounts))
	return mounts
}

// ConfigMap keys must be lowercase alphanumeric characters.
func pathToKey(path string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(path)))
}

type volumeMountSlice []corev1.VolumeMount

func (slc volumeMountSlice) Len() int {
	return len(slc)
}

func (slc volumeMountSlice) Swap(i, j int) {
	slc[i], slc[j] = slc[j], slc[i]
}

func (slc volumeMountSlice) Less(i, j int) bool {
	return slc[i].MountPath < slc[j].MountPath
}

type configMapSlice []corev1.ConfigMap

func (slc configMapSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc configMapSlice) Len() int {
	return len(slc)
}
