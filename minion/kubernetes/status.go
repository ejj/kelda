package kubernetes

import (
	"fmt"
	"sort"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	joinContainersToPods = joinContainersToPodsImpl
	statusForPod         = statusForPodImpl
	statusForContainer   = statusForContainerImpl
)

// updateContainerStatuses syncs the status of the Kubernetes pods with the
// Kelda database. If there is no pod associated with a container, it tries to
// provide other helpful information, such as whether the container is waiting
// on a secret, or waiting for an image to be built.
func updateContainerStatuses(conn db.Conn, podsClient clientv1.PodInterface,
	secretClient SecretClient) {

	pods, err := podsClient.List(metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to list current pods")
		return
	}

	conn.Txn(db.ImageTable, db.ContainerTable).Run(func(view db.Database) error {
		pairs, noInfoContainers := joinContainersToPods(
			view.SelectFromContainer(nil), pods.Items)
		for _, pair := range pairs {
			dbc := pair.L.(db.Container)
			pod := pair.R.(corev1.Pod)

			dbc.Status, dbc.Created = statusForPod(pod)
			dbc.PodName = pod.GetName()
			dbc.Minion = pod.Status.HostIP
			view.Commit(dbc)
		}

		imageMap := map[db.Image]db.Image{}
		for _, img := range view.SelectFromImage(nil) {
			imageMap[db.Image{
				Name:       img.Name,
				Dockerfile: img.Dockerfile,
			}] = img
		}

		for _, intf := range noInfoContainers {
			dbc := intf.(db.Container)
			dbc.Status = statusForContainer(imageMap, secretClient, dbc)
			// Unset the Created field in case it was set when the container
			// was running in the past.
			dbc.Created = time.Time{}
			view.Commit(dbc)
		}
		return nil
	})
}

// joinContainersToPods tries to match the given containers with the given pods.
func joinContainersToPodsImpl(dbcs []db.Container, pods []corev1.Pod) (
	pairs []join.Pair, noInfoContainers []interface{}) {
	type joinKey struct {
		Hostname              string
		Image                 string
		Command               string
		EnvHash               string
		FilepathToContentHash string
		DockerfileHash        string
	}
	dbcKey := func(intf interface{}) interface{} {
		dbc := intf.(db.Container)
		return joinKey{
			Hostname: dbc.Hostname,
			Image:    dbc.Image,
			Command:  fmt.Sprintf("%+v", dbc.Command),

			// These fields should be calculated in the same way as the
			// annotations fields in updateDeployments.
			EnvHash: hashContainerValueMap(dbc.Env),
			FilepathToContentHash: hashContainerValueMap(
				dbc.FilepathToContent),
			DockerfileHash: hashStr(dbc.Dockerfile),
		}
	}
	podKey := func(intf interface{}) interface{} {
		pod := intf.(corev1.Pod)
		if len(pod.Spec.Containers) != 1 {
			log.WithField("pod", pod.Name).Error("Pods managed by Kelda " +
				"should have exactly one container. Ignoring.")
			return nil
		}
		return joinKey{
			Hostname: pod.Spec.Hostname,
			Command: fmt.Sprintf("%v",
				pod.Spec.Containers[0].Args),
			Image:                 pod.Annotations[imageKey],
			EnvHash:               pod.Annotations[envHashKey],
			FilepathToContentHash: pod.Annotations[filesHashKey],
			DockerfileHash:        pod.Annotations[dockerfileHashKey],
		}
	}
	pairs, noInfoContainers, _ = join.HashJoin(
		db.ContainerSlice(dbcs), podSlice(pods), dbcKey, podKey)
	return pairs, noInfoContainers
}

// statusForContainer attempts to return a helpful status for why a container
// has not yet been scheduled by inspecting the image and secret statuses.
func statusForContainerImpl(imageMap map[db.Image]db.Image, secretClient SecretClient,
	dbc db.Container) (status string) {
	_, missing := toSecretHashEnvVars(secretClient, dbc.GetReferencedSecrets())
	if len(missing) != 0 {
		sort.Strings(missing)
		return fmt.Sprintf("Waiting for secrets: %v", missing)
	}

	// Check for image information.
	img, ok := imageMap[db.Image{
		Name:       dbc.Image,
		Dockerfile: dbc.Dockerfile,
	}]
	if ok {
		return img.Status
	}
	return ""
}

// statusForPod parses the status information for the given pod into a single
// string. If the status is running, it also returns when the pod was started.
func statusForPodImpl(pod corev1.Pod) (status string, createdTime time.Time) {
	// Try to get the status of the actual container.
	if len(pod.Status.ContainerStatuses) == 1 {
		status := pod.Status.ContainerStatuses[0]
		switch {
		case status.State.Running != nil:
			return "running", status.State.Running.StartedAt.Time
		case status.State.Waiting != nil:
			return "waiting: " + status.State.Waiting.Reason, time.Time{}
		case status.State.Terminated != nil:
			return "terminated: " + status.State.Terminated.Reason,
				time.Time{}
		default:
			return "unrecognized container state", time.Time{}
		}
	}

	// Check if the pod is scheduled.
	for _, status := range pod.Status.Conditions {
		if status.Status == corev1.ConditionTrue &&
			status.Type == corev1.PodScheduled {
			return "scheduled", time.Time{}
		}
	}

	return "no status information", time.Time{}
}

type podSlice []corev1.Pod

func (slc podSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc podSlice) Len() int {
	return len(slc)
}
