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

// CreatePodDisruptionBudget create a pud disruption budget telling
// k8s to avoid removing more than one node at a time
func CreatePodDisruptionBudget(cluster apiv1.Cluster) policyv1beta1.PodDisruptionBudget {
	one := intstr.FromInt(1)

	return policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"postgresql": cluster.Name,
				},
			},
			MaxUnavailable: &one,
		},
	}
}
