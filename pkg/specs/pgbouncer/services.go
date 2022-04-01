/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
