// Package scheduler is respnosible for deciding on which minion to place each container
// in the cluster.  It does this by updating each container in the Database with the
// PrivateIP of the minion it's assigned to, or the empty string if no assignment could
// be made.  Worker nodes then read these assignments form Etcd, and boot the containers
// that they are instructed to.
package scheduler

import (
	"fmt"
	"time"

	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

var c = counter.New("Scheduler")

// Run blocks implementing the scheduler module.
func Run(conn db.Conn, dk docker.Client) {
	var clientset *kubernetes.Clientset
	var watchClient watch.Interface
	for {
		var clientsetErr, watchErr error
		clientset, clientsetErr = getKubeClientset()
		if clientsetErr == nil {
			watchClient, watchErr = clientset.CoreV1().
				Pods(corev1.NamespaceDefault).Watch(metav1.ListOptions{})
			if watchErr != nil {
				log.WithError(watchErr).Error("Failed to get Kubernetes Watch client")
			}
		} else {
			log.WithError(clientsetErr).Error("Failed to get Kubernetes client")
		}

		if clientsetErr == nil && watchErr == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}

	configMapsClient := clientset.CoreV1().ConfigMaps(corev1.NamespaceDefault)
	deploymentsClient := clientset.AppsV1().Deployments(corev1.NamespaceDefault)
	nodesClient := clientset.CoreV1().Nodes()
	podsClient := clientset.CoreV1().Pods(corev1.NamespaceDefault)
	go func() {
		loopLog := util.NewEventTimer("Update containers")
		for range conn.TriggerTick(60, db.ContainerTable, db.PlacementTable,
			db.EtcdTable, db.ImageTable).C {
			loopLog.LogStart()

			if updateConfigMaps(conn, configMapsClient) {
				updateDeployments(conn, deploymentsClient)
			}
			loopLog.LogEnd()
		}
	}()

	go func() {
		loopLog := util.NewEventTimer("Update node labels")
		for range conn.TriggerTick(60, db.MinionTable, db.EtcdTable).C {
			loopLog.LogStart()
			updateNodeLabels(conn.SelectFromMinion(nil), nodesClient)
			loopLog.LogEnd()
		}
	}()

	loopLog := util.NewEventTimer("Update container statuses")
	for range watchClient.ResultChan() {
		loopLog.LogStart()
		updateContainerStatuses(conn, podsClient)
		loopLog.LogEnd()
	}
}

func getKubeClientset() (*kubernetes.Clientset, error) {
	apiConfig := api.NewConfig()
	apiConfig.Clusters["kelda"] = api.NewCluster()
	apiConfig.Clusters["kelda"].CertificateAuthority = tlsIO.CACertPath(tlsIO.MinionTLSDir)
	apiConfig.Clusters["kelda"].Server = fmt.Sprintf("http://localhost:8080")

	apiConfig.AuthInfos["tls"] = api.NewAuthInfo()
	apiConfig.AuthInfos["tls"].ClientCertificate = tlsIO.SignedCertPath(tlsIO.MinionTLSDir)
	apiConfig.AuthInfos["tls"].ClientKey = tlsIO.SignedKeyPath(tlsIO.MinionTLSDir)

	apiConfig.CurrentContext = "default"
	apiConfig.Contexts["default"] = api.NewContext()
	apiConfig.Contexts["default"].Cluster = "kelda"
	apiConfig.Contexts["default"].AuthInfo = "tls"

	clientConfig, err := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(clientConfig)
}
