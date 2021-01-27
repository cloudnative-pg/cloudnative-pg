/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// CreateClusterAnyService create a service insisting on all the pods
func CreateClusterAnyService(cluster apiv1.Cluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceAnyName(),
			Namespace: cluster.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(5432),
					Port:       5432,
				},
			},
			Selector: map[string]string{
				"postgresql": cluster.Name,
			},
		},
	}
}

// CreateClusterReadService create a service insisting on all the ready pods
func CreateClusterReadService(cluster apiv1.Cluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadName(),
			Namespace: cluster.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(5432),
					Port:       5432,
				},
			},
			Selector: map[string]string{
				"postgresql": cluster.Name,
			},
		},
	}
}

// CreateClusterReadWriteService create a service insisting on the primary pod
func CreateClusterReadWriteService(cluster apiv1.Cluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadWriteName(),
			Namespace: cluster.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(5432),
					Port:       5432,
				},
			},
			Selector: map[string]string{
				"postgresql":         cluster.Name,
				ClusterRoleLabelName: ClusterRoleLabelPrimary,
			},
		},
	}
}
