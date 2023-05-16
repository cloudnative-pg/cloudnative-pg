/*
Copyright The CloudNativePG Contributors

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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
		cluster.GetLDAPSecretName(),
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
	involvedSecretNames = append(involvedSecretNames, externalClusterSecrets(cluster)...)
	involvedSecretNames = append(involvedSecretNames, managedRolesSecrets(cluster)...)

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
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"clusters",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			ResourceNames: []string{
				cluster.Name,
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
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
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"backups",
			},
			Verbs: []string{
				"list",
				"get",
				"delete",
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
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

func externalClusterSecrets(cluster apiv1.Cluster) []string {
	var result []string

	for _, server := range cluster.Spec.ExternalClusters {
		if server.SSLCert != nil {
			result = append(result,
				server.SSLCert.Name)
		}
		if server.SSLRootCert != nil {
			result = append(result,
				server.SSLRootCert.Name)
		}
		if server.SSLKey != nil {
			result = append(result,
				server.SSLKey.Name)
		}
		if server.Password != nil {
			result = append(result,
				server.Password.Name)
		}
		if barmanObjStore := server.BarmanObjectStore; barmanObjStore != nil {
			result = append(
				result,
				s3CredentialsSecrets(barmanObjStore.BarmanCredentials.AWS)...)
			result = append(
				result,
				azureCredentialsSecrets(barmanObjStore.BarmanCredentials.Azure)...)
			result = append(
				result,
				googleCredentialsSecrets(barmanObjStore.BarmanCredentials.Google)...)
			if barmanObjStore.EndpointCA != nil {
				result = append(result, barmanObjStore.EndpointCA.Name)
			}
		}
	}

	return result
}

func backupSecrets(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) []string {
	var result []string

	// Secrets needed to access S3 and Azure
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		result = append(
			result,
			s3CredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.BarmanCredentials.AWS)...)
		result = append(
			result,
			azureCredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.BarmanCredentials.Azure)...)
		result = append(
			result,
			googleCredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.BarmanCredentials.Google)...)
	}

	// Secrets needed by Barman, if set
	if cluster.Spec.Backup.IsBarmanEndpointCASet() {
		result = append(
			result,
			cluster.Spec.Backup.BarmanObjectStore.EndpointCA.Name)
	}

	if backupOrigin != nil {
		result = append(
			result,
			s3CredentialsSecrets(backupOrigin.Status.BarmanCredentials.AWS)...)
		result = append(
			result,
			azureCredentialsSecrets(backupOrigin.Status.BarmanCredentials.Azure)...)
		result = append(
			result,
			googleCredentialsSecrets(backupOrigin.Status.BarmanCredentials.Google)...)
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

	var secrets []string

	if s3Credentials.AccessKeyIDReference != nil {
		secrets = append(secrets, s3Credentials.AccessKeyIDReference.Name)
	}

	if s3Credentials.SecretAccessKeyReference != nil {
		secrets = append(secrets, s3Credentials.SecretAccessKeyReference.Name)
	}

	return secrets
}

func googleCredentialsSecrets(googleCredentials *apiv1.GoogleCredentials) []string {
	if googleCredentials == nil {
		return nil
	}
	var secrets []string

	if googleCredentials.ApplicationCredentials != nil {
		return append(secrets, googleCredentials.ApplicationCredentials.Name)
	}

	return secrets
}

func managedRolesSecrets(cluster apiv1.Cluster) []string {
	if cluster.Spec.Managed == nil {
		return nil
	}
	managedRoles := cluster.Spec.Managed.Roles
	if len(managedRoles) == 0 {
		return nil
	}
	secretNames := make([]string, 0, len(managedRoles))
	for _, role := range managedRoles {
		if role.DisablePassword || role.PasswordSecret == nil {
			continue
		}
		secretName := role.PasswordSecret.Name
		if secretName != "" {
			secretNames = append(secretNames, secretName)
		}
	}

	return secretNames
}
