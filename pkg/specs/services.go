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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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
					TargetPort: intstr.FromInt(postgres.ServerPort),
					Port:       postgres.ServerPort,
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
					TargetPort: intstr.FromInt(postgres.ServerPort),
					Port:       postgres.ServerPort,
				},
			},
			Selector: map[string]string{
				"postgresql": cluster.Name,
			},
		},
	}
}

// CreateClusterReadOnlyService create a service insisting on all the ready pods
func CreateClusterReadOnlyService(cluster apiv1.Cluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadOnlyName(),
			Namespace: cluster.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(postgres.ServerPort),
					Port:       postgres.ServerPort,
				},
			},
			Selector: map[string]string{
				"postgresql":         cluster.Name,
				ClusterRoleLabelName: ClusterRoleLabelReplica,
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
					TargetPort: intstr.FromInt(postgres.ServerPort),
					Port:       postgres.ServerPort,
				},
			},
			Selector: map[string]string{
				"postgresql":         cluster.Name,
				ClusterRoleLabelName: ClusterRoleLabelPrimary,
			},
		},
	}
}
