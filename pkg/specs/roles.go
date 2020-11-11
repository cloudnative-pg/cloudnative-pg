/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
)

// CreateRole create a role with the permissions needed by the instance manager
func CreateRole(cluster v1alpha1.Cluster) rbacv1.Role {
	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"configmaps",
				},
				Verbs: []string{
					"get",
					"watch",
				},
				ResourceNames: []string{
					cluster.Name,
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
				ResourceNames: []string{
					cluster.GetCASecretName(),
					cluster.GetServerSecretName(),
					cluster.GetPostgresTLSSecretName(),
				},
			},
			{
				APIGroups: []string{
					"postgresql.k8s.enterprisedb.io",
				},
				Resources: []string{
					"clusters",
				},
				Verbs: []string{
					"get",
					"watch",
				},
				ResourceNames: []string{
					cluster.Name,
				},
			},
			{
				APIGroups: []string{
					"postgresql.k8s.enterprisedb.io",
				},
				Resources: []string{
					"clusters/status",
				},
				Verbs: []string{
					"get",
					"patch",
					"update",
					"watch",
				},
				ResourceNames: []string{
					cluster.Name,
				},
			},
			{
				APIGroups: []string{
					"postgresql.k8s.enterprisedb.io",
				},
				Resources: []string{
					"backups",
				},
				Verbs: []string{
					"get",
				},
			},
			{
				APIGroups: []string{
					"postgresql.k8s.enterprisedb.io",
				},
				Resources: []string{
					"backups/status",
				},
				Verbs: []string{
					"get",
					"patch",
					"update",
				},
			},
		},
	}
}
