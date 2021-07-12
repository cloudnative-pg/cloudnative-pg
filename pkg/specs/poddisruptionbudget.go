/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// BuildReplicasPodDisruptionBudget creates a pod disruption budget telling
// K8s to avoid removing more than one replica at a time
func BuildReplicasPodDisruptionBudget(cluster *apiv1.Cluster) *policyv1beta1.PodDisruptionBudget {
	// We should ensure that in a cluster of n instances,
	// with n-1 replicas, at least n-2 are always available
	if cluster == nil || cluster.Spec.Instances < 3 {
		return nil
	}
	minAvailableReplicas := int(cluster.Spec.Instances) - 2
	allReplicasButOne := intstr.FromInt(minAvailableReplicas)

	return &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ClusterLabelName:     cluster.Name,
					ClusterRoleLabelName: ClusterRoleLabelReplica,
				},
			},
			MinAvailable: &allReplicasButOne,
		},
	}
}

// BuildPrimaryPodDisruptionBudget creates a pod disruption budget, telling
// K8s to avoid removing more than one primary instance at a time
func BuildPrimaryPodDisruptionBudget(cluster *apiv1.Cluster) *policyv1beta1.PodDisruptionBudget {
	if cluster == nil {
		return nil
	}
	one := intstr.FromInt(1)

	return &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-primary",
			Namespace: cluster.Namespace,
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					ClusterLabelName:     cluster.Name,
					ClusterRoleLabelName: ClusterRoleLabelPrimary,
				},
			},
			MinAvailable: &one,
		},
	}
}
