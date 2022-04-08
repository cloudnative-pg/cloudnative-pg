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

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	pgBouncerConfig "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/pgbouncer/config"
)

// Service create the specification for the service of
// pgbouncer
func Service(pooler *apiv1.Pooler) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pooler.Name,
			Namespace: pooler.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "pgbouncer",
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(pgBouncerConfig.PgBouncerPort),
					Port:       pgBouncerConfig.PgBouncerPort,
				},
			},
			Selector: map[string]string{
				PgbouncerNameLabel: pooler.Name,
			},
		},
	}
}
