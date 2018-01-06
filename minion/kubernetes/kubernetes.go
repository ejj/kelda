package kubernetes

import (
	"time"

	cliPath "github.com/kelda/kelda/cli/path"
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

var c = counter.New("Kubernetes")

// Run converts the containers specified by the user into deployments in the
// Kubernetes cluster. It also syncs the status of the deployment into the
// database.
// The module is implemented as several goroutines. One goroutine creates the
// ConfigMap and deployment objects for Kubernetes to deploy. Another goroutine
// tags the Kubernetes workers with metadata to be used by placement rules. The
// final goroutine syncs the status of the deployment into the database.
func Run(conn db.Conn, dk docker.Client) {
	var clientset *kubernetes.Clientset
	var err error
	for {
		clientset, err = newClientset()
		if err == nil {
			break
		}
		log.WithError(err).Error("Failed to get Kubernetes client")
		time.Sleep(5 * time.Second)
	}

	var podWatcher, secretWatcher watch.Interface
	for {
		podWatcher, err = clientset.CoreV1().
			Pods(corev1.NamespaceDefault).Watch(metav1.ListOptions{})
		if err == nil {
			break
		}
		log.WithError(err).Debug("Failed to get Kubernetes pod watcher. This " +
			"is expected while the kube-apiserver container is first " +
			"booting.")
		time.Sleep(5 * time.Second)
	}
	for {
		secretWatcher, err = clientset.CoreV1().
			Secrets(corev1.NamespaceDefault).Watch(metav1.ListOptions{})
		if err == nil {
			break
		}
		// Given that we succeeded to get the podwatcher, getting the
		// secretWatcher shouldn't fail.
		log.WithError(err).Error("Failed to get Kubernetes secret watcher")
		time.Sleep(5 * time.Second)
	}

	configMapsClient := clientset.CoreV1().ConfigMaps(corev1.NamespaceDefault)
	deploymentsClient := clientset.AppsV1().Deployments(corev1.NamespaceDefault)
	nodesClient := clientset.CoreV1().Nodes()
	podsClient := clientset.CoreV1().Pods(corev1.NamespaceDefault)
	secretClient := secretClientImpl{
		clientset.CoreV1().Secrets(corev1.NamespaceDefault)}
	go func() {
		trig := util.JoinNotifiers(toStructChan(secretWatcher.ResultChan()),
			conn.TriggerTick(60, db.ContainerTable, db.PlacementTable,
				db.EtcdTable, db.ImageTable).C)
		for range trig {
			// Update config maps before updating deployments. This way, any
			// config maps referenced in updateDeployments will most likely
			// exist.
			if updateConfigMaps(conn, configMapsClient) {
				updateDeployments(conn, deploymentsClient, secretClient)
			}
		}
	}()

	go func() {
		for range conn.TriggerTick(60, db.MinionTable, db.EtcdTable).C {
			updateNodeLabels(conn.SelectFromMinion(nil), nodesClient)
		}
	}()

	trig := util.JoinNotifiers(toStructChan(podWatcher.ResultChan()),
		toStructChan(secretWatcher.ResultChan()),
		conn.TriggerTick(60, db.ImageTable, db.ContainerTable).C)
	for range trig {
		updateContainerStatuses(conn, podsClient, secretClient)
	}
}

// NewKubeconfig returns a Kubeconfig setup to connect to the given server, and
// using the Kelda authentication certificates.
func NewKubeconfig(server string) api.Config {
	apiConfig := api.NewConfig()
	apiConfig.Clusters["kelda"] = api.NewCluster()
	apiConfig.Clusters["kelda"].CertificateAuthority =
		tlsIO.CACertPath(cliPath.MinionTLSDir)
	apiConfig.Clusters["kelda"].Server = server

	apiConfig.AuthInfos["tls"] = api.NewAuthInfo()
	apiConfig.AuthInfos["tls"].ClientCertificate =
		tlsIO.SignedCertPath(cliPath.MinionTLSDir)
	apiConfig.AuthInfos["tls"].ClientKey =
		tlsIO.SignedKeyPath(cliPath.MinionTLSDir)

	apiConfig.CurrentContext = "default"
	apiConfig.Contexts["default"] = api.NewContext()
	apiConfig.Contexts["default"].Cluster = "kelda"
	apiConfig.Contexts["default"].AuthInfo = "tls"
	return *apiConfig
}

func newClientset() (*kubernetes.Clientset, error) {
	kubeconfig := NewKubeconfig("http://localhost:8080")
	defaultClientConfig := clientcmd.NewDefaultClientConfig(kubeconfig,
		&clientcmd.ConfigOverrides{})
	clientConfig, err := defaultClientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(clientConfig)
}

func toStructChan(watchChan <-chan watch.Event) chan struct{} {
	c := make(chan struct{}, 1)
	go func() {
		for range watchChan {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	}()
	return c
}
