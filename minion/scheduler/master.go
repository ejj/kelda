package scheduler

import (
	"fmt"

	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

type runnableContainer struct {
	resolvedImage string
	db.Container
}

func runMaster(conn db.Conn) {
	if !conn.EtcdLeader() {
		return
	}

	apiConfig := api.NewConfig()
	apiConfig.Clusters["kelda"] = api.NewCluster()
	apiConfig.Clusters["kelda"].CertificateAuthority = tlsIO.CACertPath(tlsIO.MinionTLSDir)
	apiConfig.Clusters["kelda"].Server = fmt.Sprintf("http://%s:8080", conn.MinionSelf().PrivateIP)

	apiConfig.AuthInfos["tls"] = api.NewAuthInfo()
	apiConfig.AuthInfos["tls"].ClientCertificate = tlsIO.SignedCertPath(tlsIO.MinionTLSDir)
	apiConfig.AuthInfos["tls"].ClientKey = tlsIO.SignedKeyPath(tlsIO.MinionTLSDir)

	apiConfig.CurrentContext = "default"
	apiConfig.Contexts["default"] = api.NewContext()
	apiConfig.Contexts["default"].Cluster = "kelda"
	apiConfig.Contexts["default"].AuthInfo = "tls"

	clientConfig, err := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		log.WithError(err).Error("Failed to generate Kubernetes access config")
		return
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		log.WithError(err).Error("Failed to get Kubernetes client")
		return
	}

	// TODO: Kubernetes changes don't propogate fast enough for the second
	// iteration to see them. E.g. when stopping containers, they will be
	// stopped multiple times. We should either make the Create and Delete
	// calls block somehow, or rely on the Kubernetes Watch interface to
	// respond to changes.
	for i := 0; i < 2; i++ {
		podsClient := clientset.CoreV1().Pods(corev1.NamespaceDefault)
		podsList, err := podsClient.List(metav1.ListOptions{})
		if err != nil {
			log.WithError(err).Error("Failed to get current pods")
			return
		}

		var toBoot, toRemove []interface{}
		conn.Txn(db.ContainerTable, db.ImageTable, db.MinionTable).Run(
			func(view db.Database) error {
				// TODO: Implement placement.
				images := view.SelectFromImage(nil)
				containers := view.SelectFromContainer(nil)

				imageMap := map[db.Image]db.Image{}
				for _, img := range images {
					imageMap[db.Image{
						Name:       img.Name,
						Dockerfile: img.Dockerfile,
					}] = img
				}

				var runnableContainers []runnableContainer
				for _, dbc := range containers {
					// If the container isn't built by Kelda, the image doesn't
					// have to be rewritten to the version hosted by the local
					// registry.
					if dbc.Dockerfile == "" {
						runnableContainers = append(runnableContainers, runnableContainer{
							resolvedImage: dbc.Image,
							Container:     dbc,
						})
						continue
					}

					img, ok := imageMap[db.Image{
						Name:       dbc.Image,
						Dockerfile: dbc.Dockerfile,
					}]
					if !ok {
						continue
					}

					if dbc.Status != img.Status {
						dbc.Status = img.Status
						view.Commit(dbc)
					}

					if img.Status == db.Built {
						runnableContainers = append(runnableContainers, runnableContainer{
							resolvedImage: img.RepoDigest,
							Container:     dbc,
						})
					}
				}

				type joinKey struct {
					// TODO: Respect filepathToContent
					Hostname, Image, Command, Env string
				}
				podKeyFn := func(podIntf interface{}) interface{} {
					// There is guaranteed to be exactly one container because we
					// pre-process the join list.
					container := podIntf.(corev1.Pod).Spec.Containers[0]
					env := map[string]string{}
					for _, envVar := range container.Env {
						env[envVar.Name] = envVar.Value
					}
					return joinKey{
						Hostname: container.Name,
						Image:    container.Image,
						Command:  fmt.Sprintf("%v", container.Command),
						Env:      str.MapAsString(env),
					}
				}
				dbcKeyFn := func(dbcIntf interface{}) interface{} {
					dbc := dbcIntf.(runnableContainer)
					env := map[string]string{}
					for k, v := range dbc.Env {
						// TODO: evaluate other vals
						if valStr, ok := v.Value.(string); ok {
							env[k] = valStr
						}
					}
					return joinKey{
						Hostname: dbc.Hostname,
						Image:    dbc.resolvedImage,
						Command:  fmt.Sprintf("%v", dbc.Command),
						Env:      str.MapAsString(env),
					}
				}

				var malformedPods []interface{}
				var currentPods []corev1.Pod
				for _, pod := range podsList.Items {
					if len(pod.Spec.Containers) != 1 ||
						len(pod.Status.ContainerStatuses) > 1 {
						log.WithField("name", pod.Name).Warn(
							"Unexpected pod configuration. Kelda expects pods to " +
								"contain exactly one container. The pod will be removed.")
						malformedPods = append(malformedPods, pod)
						continue
					}
					currentPods = append(currentPods, pod)
				}
				var pairs []join.Pair
				pairs, toBoot, toRemove = join.HashJoin(
					runnableContainerSlice(runnableContainers),
					podSlice(podsList.Items),
					dbcKeyFn, podKeyFn)
				toRemove = append(toRemove, malformedPods...)

				for _, pair := range pairs {
					dbc := pair.L.(runnableContainer).Container
					pod := pair.R.(corev1.Pod)

					setContainerStatus(&dbc, pod)
					dbc.PodID = pod.GetName()
					dbc.Minion = pod.Status.HostIP
					view.Commit(dbc)
				}
				return nil
			})
		if len(toBoot) == 0 && len(toRemove) == 0 {
			break
		}

		for _, podIntf := range toRemove {
			name := podIntf.(corev1.Pod).ObjectMeta.Name
			log.WithField("pod", name).Info("Deleting pod")
			if err := podsClient.Delete(name, nil); err != nil {
				log.WithError(err).WithField("pod", name).Error("Failed to delete pod")
			}
		}

		for _, dbcIntf := range toBoot {
			dbc := dbcIntf.(runnableContainer)
			log.WithField("pod", dbc.Hostname).Info("Creating pod")
			pod := containerToPod(dbc)
			if _, err := podsClient.Create(&pod); err != nil {
				log.WithError(err).WithField("pod", dbc.Hostname).
					Error("Failed to create pod")
			}
		}
	}
}

func containerToPod(dbc runnableContainer) corev1.Pod {
	var env []corev1.EnvVar
	for k, v := range dbc.Env {
		// TODO: evaluate other vals
		if valStr, ok := v.Value.(string); ok {
			env = append(env, corev1.EnvVar{Name: k, Value: valStr})
		}
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: dbc.Hostname,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					// TODO: Respect filepathToContent
					Name:  dbc.Hostname,
					Image: dbc.resolvedImage,
					// TODO: Command vs Args?
					Command: dbc.Command,
					Env:     env,
				},
			},
		},
	}
}

func setContainerStatus(dbc *db.Container, pod corev1.Pod) {
	if len(pod.Status.ContainerStatuses) == 1 {
		// Get the status of the actual container.
		status := pod.Status.ContainerStatuses[0]
		switch {
		case status.State.Running != nil:
			dbc.Status = "running"
			dbc.Created = status.State.Running.StartedAt.Time
		case status.State.Waiting != nil:
			dbc.Status = "waiting: " + status.State.Waiting.Reason
		case status.State.Terminated != nil:
			dbc.Status = "terminated: " + status.State.Terminated.Reason
		}
	} else {
		// Check if the pod is scheduled.
		for _, status := range pod.Status.Conditions {
			if status.Status == corev1.ConditionTrue &&
				status.Type == corev1.PodScheduled {
				dbc.Status = "scheduled"
			}
		}
	}
}

type podSlice []corev1.Pod

func (slc podSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc podSlice) Len() int {
	return len(slc)
}

type runnableContainerSlice []runnableContainer

func (slc runnableContainerSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc runnableContainerSlice) Len() int {
	return len(slc)
}
