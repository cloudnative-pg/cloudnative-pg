/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// ServiceAccount creates a service account for a given pooler
func ServiceAccount(pooler *apiv1.Pooler) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name: pooler.Name, Namespace: pooler.Namespace,
	}}
}

// Role creates a role for a given pooler
func Role(pooler *apiv1.Pooler) *v1.Role {
	secretNames := []string{pooler.GetAuthQuerySecretName()}
	if pooler.Status.Secrets != nil {
		if pooler.Status.Secrets.ServerCA.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ServerCA.Name)
		}

		if pooler.Status.Secrets.ServerTLS.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ServerTLS.Name)
		}

		if pooler.Status.Secrets.ClientCA.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ClientCA.Name)
		}
	}

	return &v1.Role{ObjectMeta: metav1.ObjectMeta{
		Name: pooler.Name, Namespace: pooler.Namespace,
	}, Rules: []v1.PolicyRule{
		{
			APIGroups: []string{
				"postgresql.k8s.enterprisedb.io",
			},
			Resources: []string{
				"poolers",
			},
			Verbs: []string{
				"get",
				"watch",
			},
			ResourceNames: []string{
				pooler.Name,
			},
		},
		{
			APIGroups: []string{
				"postgresql.k8s.enterprisedb.io",
			},
			Resources: []string{
				"poolers/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
				"watch",
			},
			ResourceNames: []string{
				pooler.Name,
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"secrets",
			},
			Verbs: []string{
				"get",
				"watch",
			},
			ResourceNames: secretNames,
		},
	}}
}

// RoleBinding creates a role binding for a given pooler
func RoleBinding(pooler *apiv1.Pooler) v1.RoleBinding {
	return specs.CreateRoleBinding(pooler.ObjectMeta)
}
