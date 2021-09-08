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
func CreateRole(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) rbacv1.Role {
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

	involvedSecretNames = append(involvedSecretNames, backupSecrets(cluster, backupOrigin)...)

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

func backupSecrets(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) []string {
	var result []string

	// Secrets needed to access S3 and Azure
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		result = append(
			result,
			s3CredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.S3Credentials)...)
		result = append(
			result,
			azureCredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.AzureCredentials)...)
	}

	if backupOrigin != nil {
		result = append(
			result,
			s3CredentialsSecrets(backupOrigin.Status.S3Credentials)...)
		result = append(
			result,
			azureCredentialsSecrets(backupOrigin.Status.AzureCredentials)...)
	}

	for _, externalCluster := range cluster.Spec.ExternalClusters {
		if externalCluster.BarmanObjectStore != nil {
			result = append(
				result,
				s3CredentialsSecrets(externalCluster.BarmanObjectStore.S3Credentials)...)
			result = append(
				result,
				azureCredentialsSecrets(externalCluster.BarmanObjectStore.AzureCredentials)...)
		}
	}

	return result
}

func azureCredentialsSecrets(azureCredentials *apiv1.AzureCredentials) []string {
	var result []string

	if azureCredentials == nil {
		return nil
	}

	if azureCredentials.ConnectionString != nil {
		result = append(result,
			azureCredentials.ConnectionString.Name)
	}
	if azureCredentials.StorageAccount != nil {
		result = append(result,
			azureCredentials.StorageAccount.Name)
	}
	if azureCredentials.StorageKey != nil {
		result = append(result,
			azureCredentials.StorageKey.Name)
	}

	if azureCredentials.StorageSasToken != nil {
		result = append(result,
			azureCredentials.StorageSasToken.Name)
	}
	return result
}

func s3CredentialsSecrets(s3Credentials *apiv1.S3Credentials) []string {
	if s3Credentials == nil {
		return nil
	}

	return []string{
		s3Credentials.SecretAccessKeyReference.Name,
		s3Credentials.AccessKeyIDReference.Name,
	}
}
