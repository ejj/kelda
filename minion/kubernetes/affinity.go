package kubernetes

import (
	"errors"
	"sort"

	"github.com/kelda/kelda/db"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"
)

const providerKey = "kelda.io/host.provider"
const regionKey = "kelda.io/host.region"
const sizeKey = "kelda.io/host.size"
const floatingIPKey = "kelda.io/host.floatingIP"

// toAffinities converts the Kelda placement rules into the format expected by
// the Kubernetes deployment engine. It aggregates all of the placement rules
// for each TargetContainer into a single Kubernetes Affinity rule. The
// placements in terms of other containers are combined under the
// Affinity.PodAntiAffinity field, and the machine placements are combined
// under Affinity.NodeAffinity.
// The labels referenced in Affinity.NodeAffinity are managed by the
// updateNodeLabels function.
func toAffinities(placements []db.Placement) map[string]*corev1.Affinity {
	// Sort the placements so the generated affinities will be consistent.
	// Otherwise, Kubernetes might treat a difference in affinity term
	// orderings as a different deployment spec, and restart the existing
	// deployment unnecessarily.
	sort.Sort(db.PlacementSlice(placements))

	targetToAffinity := map[string]*corev1.Affinity{}
	for _, plcm := range placements {
		affinity, ok := targetToAffinity[plcm.TargetContainer]
		if !ok {
			affinity = &corev1.Affinity{}
			targetToAffinity[plcm.TargetContainer] = affinity
		}

		if plcm.OtherContainer != "" {
			handlePodAffinity(affinity, plcm.OtherContainer, plcm.Exclusive)
		}

		nodeConstraints := []struct{ key, val string }{
			{providerKey, plcm.Provider},
			{regionKey, plcm.Region},
			{sizeKey, plcm.Size},
			{floatingIPKey, plcm.FloatingIP},
		}
		for _, nodeConstraint := range nodeConstraints {
			if nodeConstraint.val == "" {
				continue
			}
			handleNodeAffinity(affinity, nodeConstraint.key,
				nodeConstraint.val, plcm.Exclusive)
		}
	}
	return targetToAffinity
}

// handlePodAffinity modifies the given affinity to account for the given pod
// placement constraint.
func handlePodAffinity(affinity *corev1.Affinity, otherContainer string, exclusive bool) {
	if exclusive {
		if affinity.PodAntiAffinity == nil {
			affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		term := corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					hostnameKey: otherContainer,
				},
			},
			TopologyKey: "kubernetes.io/hostname",
		}
		podAffinities := &affinity.PodAntiAffinity.
			RequiredDuringSchedulingIgnoredDuringExecution
		*podAffinities = append(*podAffinities, term)
	} else {
		// XXX: This is not difficult to implement with
		// Kubernetes, but it was not supported in the
		// original Kelda scheduler.
		log.WithField("otherContainer", otherContainer).Warning(
			"Kelda currently does not support inclusive " +
				"container placement constraints")
	}
}

// handleNodeAffinity modifies the given affinity to account for the given node
// placement constraint.
func handleNodeAffinity(affinity *corev1.Affinity, key, value string, exclusive bool) {
	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &corev1.NodeAffinity{}
		ref := &affinity.NodeAffinity.
			RequiredDuringSchedulingIgnoredDuringExecution
		*ref = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{}},
		}
	}

	operator := corev1.NodeSelectorOpIn
	if exclusive {
		operator = corev1.NodeSelectorOpNotIn
	}
	match := corev1.NodeSelectorRequirement{
		Key:      key,
		Operator: operator,
		Values:   []string{value},
	}

	matchExpressions := &affinity.NodeAffinity.
		RequiredDuringSchedulingIgnoredDuringExecution.
		NodeSelectorTerms[0].MatchExpressions
	*matchExpressions = append(*matchExpressions, match)
}

// updateNodeLabels should be called regularly to sync the metadata about nodes
// to Kubernetes so that they can be referenced in NodeAffinities. These
// affinities are created in the toAffinities function.
func updateNodeLabels(nodes []db.Minion, nodesClient clientv1.NodeInterface) {
	nodeToLabels := map[string]map[string]string{}
	for _, node := range nodes {
		nodeToLabels[node.PrivateIP] = map[string]string{
			providerKey:   node.Provider,
			regionKey:     node.Region,
			sizeKey:       node.Size,
			floatingIPKey: node.FloatingIP,
		}
	}

	nodesList, err := nodesClient.List(metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to get current nodes")
		return
	}

	for _, node := range nodesList.Items {
		privateIP, err := getPrivateIP(node)
		if err != nil {
			log.WithError(err).WithField("node", node.Name).Error(
				"Failed to get private IP")
			continue
		}

		labels, ok := nodeToLabels[privateIP]
		if !ok {
			continue
		}

		var needsUpdate bool
		for key, exp := range labels {
			actual, ok := node.Labels[key]
			if !ok || exp != actual {
				node.Labels[key] = exp
				needsUpdate = true
			}
		}

		if needsUpdate {
			c.Inc("Update node labels")
			log.WithField("node", node.Name).WithField("labels", labels).
				Info("Updating node labels")
			// Retry updating the labels if the apiserver reports that there's
			// a conflict. Conflicts are benign -- for example, there might be
			// a conflict if Kubernetes updated the node to change its
			// connection status.
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				_, err := nodesClient.Update(&node)
				return err
			})
			if err != nil {
				log.WithError(err).Error("Failed to update node labels")
			}
		}
	}
}

func getPrivateIP(node corev1.Node) (string, error) {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address, nil
		}
	}
	return "", errors.New("no private address")
}
