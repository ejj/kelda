package scheduler

import (
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/retry"
)

func updateDeployments(conn db.Conn, deploymentsClient clientv1.DeploymentInterface) {
	currentDeployments, err := deploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to list current deployments")
		return
	}

	key := func(intf interface{}) interface{} {
		return intf.(appsv1.Deployment).Name
	}
	pairs, toCreate, toDelete := join.HashJoin(
		deploymentSlice(makeDesiredDeployments(conn)),
		deploymentSlice(currentDeployments.Items),
		key, key)

	for _, pair := range pairs {
		// Retry updating the deployment if the apiserver reports that there's
		// a conflict. Conflicts are benign -- for example, there might be a
		// conflict if Kubernetes updated the deployment to change the pod
		// status.
		deployment := pair.L.(appsv1.Deployment)
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			_, err := deploymentsClient.Update(&deployment)
			return err
		})
		if err != nil {
			log.WithError(err).WithField("deployment", deployment.Name).
				Error("Failed to update deployment")
		}
	}

	for _, intf := range toCreate {
		deployment := intf.(appsv1.Deployment)
		log.WithField("deployment", deployment.Name).
			Info("Creating deployment")
		if _, err := deploymentsClient.Create(&deployment); err != nil {
			log.WithError(err).WithField("deployment", deployment.Name).
				Error("Failed to create deployment")
		}
	}

	for _, intf := range toDelete {
		deployment := intf.(appsv1.Deployment)
		log.WithField("deployment", deployment.Name).
			Info("Deleting deployment")
		err := deploymentsClient.Delete(deployment.Name, &metav1.DeleteOptions{})
		if err != nil {
			log.WithError(err).WithField("deployment", deployment.Name).
				Error("Failed to delete deployment")
		}
	}
}

const hostnameKey = "hostname"

func makeDesiredDeployments(conn db.Conn) (deployments []appsv1.Deployment) {
	var containers []db.Container
	var images []db.Image
	var idToAffinity map[string]*corev1.Affinity
	conn.Txn(db.ContainerTable, db.ImageTable, db.PlacementTable).Run(func(view db.Database) error {
		containers = view.SelectFromContainer(nil)
		images = view.SelectFromImage(nil)
		idToAffinity = toAffinities(view.SelectFromPlacement(nil))
		return nil
	})

	imageMap := map[db.Image]db.Image{}
	for _, img := range images {
		imageMap[db.Image{
			Name:       img.Name,
			Dockerfile: img.Dockerfile,
		}] = img
	}

	for _, dbc := range containers {
		// If the container isn't built by Kelda, the image doesn't
		// have to be rewritten to the version hosted by the local
		// registry.
		image := dbc.Image
		if dbc.Dockerfile != "" {
			img, ok := imageMap[db.Image{
				Name:       dbc.Image,
				Dockerfile: dbc.Dockerfile,
			}]
			if !ok || img.Status != db.Built {
				continue
			}
			image = img.RepoDigest
		}

		var env []corev1.EnvVar
		for k, v := range dbc.Env {
			// TODO: evaluate other vals
			if valStr, ok := v.Value.(string); ok {
				env = append(env, corev1.EnvVar{Name: k, Value: valStr})
			}
		}
		pod := corev1.PodSpec{
			Hostname: dbc.Hostname,
			Containers: []corev1.Container{
				{
					Name:  dbc.Hostname,
					Image: image,
					// TODO: Command vs Args?
					Command: dbc.Command,
					Env:     env,
				},
			},
			Affinity: idToAffinity[dbc.Hostname],
		}

		if len(dbc.FilepathToContent) != 0 {
			fm := newFileMap(dbc.FilepathToContent)
			pod.Volumes = []corev1.Volume{fm.volume()}
			pod.Containers[0].VolumeMounts = fm.volumeMounts()
		}

		hostnameLabel := map[string]string{hostnameKey: dbc.Hostname}
		deployments = append(deployments, appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: dbc.Hostname,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   dbc.Hostname,
						Labels: hostnameLabel,
					},
					Spec: pod,
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: hostnameLabel,
				},
			},
		})
	}
	return deployments
}

type deploymentSlice []appsv1.Deployment

func (slc deploymentSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc deploymentSlice) Len() int {
	return len(slc)
}
