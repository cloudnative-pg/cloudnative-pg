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
func CreateRole(cluster apiv1.Cluster) rbacv1.Role {
	involvedSecretNames := []string{
		cluster.GetReplicationSecretName(),
		cluster.GetClientCASecretName(),
		cluster.GetServerCASecretName(),
		cluster.GetServerTLSSecretName(),
		cluster.GetApplicationSecretName(),
		cluster.GetSuperuserSecretName(),
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

	involvedSecretNames = append(involvedSecretNames, backupSecrets(cluster)...)

	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.PgBaseBackup != nil {
		server, _ := cluster.ExternalServer(cluster.Spec.Bootstrap.PgBaseBackup.Source)
		if server.SSLCert != nil {
			involvedSecretNames = append(involvedSecretNames,
				server.SSLCert.Name)
		}
		if server.SSLRootCert != nil {
			involvedSecretNames = append(involvedSecretNames,
				server.SSLRootCert.Name)
		}
		if server.SSLKey != nil {
			involvedSecretNames = append(involvedSecretNames,
				server.SSLKey.Name)
		}
		if server.Password != nil {
			involvedSecretNames = append(involvedSecretNames,
				server.Password.Name)
		}
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

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Rules: rules,
	}
}

func backupSecrets(cluster apiv1.Cluster) []string {
	var result []string

	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		return nil
	}

	// Secrets needed to access S3
	if cluster.Spec.Backup.BarmanObjectStore.S3Credentials != nil {
		result = append(result,
			cluster.Spec.Backup.BarmanObjectStore.S3Credentials.SecretAccessKeyReference.Name,
			cluster.Spec.Backup.BarmanObjectStore.S3Credentials.AccessKeyIDReference.Name)
	}

	// Secrets needed to access Azure
	if cluster.Spec.Backup.BarmanObjectStore.AzureCredentials != nil {
		if cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.ConnectionString != nil {
			result = append(result,
				cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.ConnectionString.Name)
		}
		if cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageAccount != nil {
			result = append(result,
				cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageAccount.Name)
		}
		if cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageKey != nil {
			result = append(result,
				cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageKey.Name)
		}

		if cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageSasToken != nil {
			result = append(result,
				cluster.Spec.Backup.BarmanObjectStore.AzureCredentials.StorageSasToken.Name)
		}
	}

	return result
}
