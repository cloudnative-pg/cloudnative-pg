/*
Copyright 2019-2022 The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
			Name:      cluster.Name + apiv1.PrimaryPodDisruptionBudgetSuffix,
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
