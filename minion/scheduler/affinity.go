package scheduler

import (
	"errors"

	"github.com/kelda/kelda/db"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const providerKey = "kelda.io/host.provider"
const regionKey = "kelda.io/host.region"
const sizeKey = "kelda.io/host.size"
const floatingIPKey = "kelda.io/host.floatingIP"

func toAffinities(placements []db.Placement) map[string]*corev1.Affinity {
	result := map[string]*corev1.Affinity{}
	for _, plcm := range placements {
		affinity, ok := result[plcm.TargetContainer]
		if !ok {
			affinity = &corev1.Affinity{}
			result[plcm.TargetContainer] = affinity
		}

		if plcm.OtherContainer != "" {
			if plcm.Exclusive {
				if affinity.PodAntiAffinity == nil {
					affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
				}

				req := corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							hostnameKey: plcm.OtherContainer,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				}
				podAffinities := &affinity.PodAntiAffinity.
					RequiredDuringSchedulingIgnoredDuringExecution
				*podAffinities = append(*podAffinities, req)
			} else {
				// XXX: This is not difficult to implement with Kubernetes, but
				// it was not supported in the original Kelda scheduler.
				log.WithField("constraint", plcm).Warning(
					"Kelda currently does not support inclusive" +
						" container placement constraints")
			}
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

			if affinity.NodeAffinity == nil {
				affinity.NodeAffinity = &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{},
						},
					},
				}
			}

			operator := corev1.NodeSelectorOpIn
			if plcm.Exclusive {
				operator = corev1.NodeSelectorOpNotIn
			}
			req := corev1.NodeSelectorRequirement{
				Key:      nodeConstraint.key,
				Operator: operator,
				Values:   []string{nodeConstraint.val},
			}

			matchExpressions := &affinity.NodeAffinity.
				RequiredDuringSchedulingIgnoredDuringExecution.
				NodeSelectorTerms[0].MatchExpressions
			*matchExpressions = append(*matchExpressions, req)
		}
	}
	return result
}

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
		}

		labels, ok := nodeToLabels[privateIP]
		if !ok {
			continue
		}

		needsUpdate := len(labels) != len(node.Labels)
		if !needsUpdate {
			for k, exp := range labels {
				if actual, ok := node.Labels[k]; !ok || exp != actual {
					needsUpdate = true
					break
				}
			}
		}

		if needsUpdate {
			log.WithField("node", node.Name).WithField("labels", labels).
				Info("Updating node labels")
			node.Labels = labels
			if _, err := nodesClient.Update(&node); err != nil {
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
