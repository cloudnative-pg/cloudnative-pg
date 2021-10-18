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
	}}
}

// RoleBinding creates a role binding for a given pooler
func RoleBinding(pooler *apiv1.Pooler) v1.RoleBinding {
	return specs.CreateRoleBinding(pooler.ObjectMeta)
}
