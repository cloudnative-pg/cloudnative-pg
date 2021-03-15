/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// CreateRole create a role with the permissions needed by the instance manager
func CreateRole(cluster apiv1.Cluster, openshift bool) rbacv1.Role {
	involvedSecretNames := []string{
		cluster.GetCASecretName(),
		cluster.GetServerSecretName(),
		cluster.GetReplicationSecretName(),
	}

	involvedConfigMapNames := []string{
		cluster.Name,
	}

	if cluster.Spec.Monitoring != nil {
		// If custom queries are used, the instance manager need privileges to read those
		// entries
		for _, secretName := range cluster.Spec.Monitoring.CustomQueriesSecret {
			involvedSecretNames = append(involvedSecretNames, secretName.Name)
		}

		for _, configMapName := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
			involvedConfigMapNames = append(involvedConfigMapNames, configMapName.Name)
		}
	}

	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		// If there is a backup section, the instance manager will need to access
		// the S3 secret too
		involvedSecretNames = append(involvedSecretNames,
			cluster.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Name,
			cluster.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Name)
	}

	rules := []rbacv1.PolicyRule{
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
			ResourceNames: involvedConfigMapNames,
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
			ResourceNames: involvedSecretNames,
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
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"events",
			},
			Verbs: []string{
				"create",
				"patch",
			},
		},
	}

	if openshift {
		openshiftRules := rbacv1.PolicyRule{
			APIGroups:     []string{"security.openshift.io"},
			Resources:     []string{"securitycontextconstraints"},
			Verbs:         []string{"use"},
			ResourceNames: []string{"nonroot"},
		}
		rules = append(rules, openshiftRules)
	}

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Rules: rules,
	}
}
