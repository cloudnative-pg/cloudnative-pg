/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

// CreatePostgresConfigMap create a configMap for this cluster
func CreatePostgresConfigMap(cluster *v1alpha1.Cluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			"postgresConfiguration": strings.Join(cluster.Spec.PostgresConfiguration.Parameters, "\n"),
			"postgresHBA":           strings.Join(cluster.Spec.PostgresConfiguration.PgHBA, "\n"),
		},
	}
}
