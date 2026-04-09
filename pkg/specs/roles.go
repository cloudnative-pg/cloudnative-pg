/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package specs

import (
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// RoleOptions are the information needed to create the Role object
// with the permissions used by the CNPG instance manager
type RoleOptions struct {
	// Cluster is the cluster that should be used to generate
	// the permissions
	Cluster *apiv1.Cluster

	// BackupOrigin is the backup object that is being used to restore
	// this cluster, if any.
	BackupOrigin *apiv1.Backup

	// Roles is the list of PostgreSQL roles. It is used to grant
	// the instance manager permissions to read the secrets that
	// contain the roles' password.
	Roles []apiv1.DatabaseRole
}

// CreateRole create a role with the permissions needed by the instance manager
func CreateRole(opts RoleOptions) rbacv1.Role {
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
			ResourceNames: getInvolvedConfigMapNames(opts.Cluster),
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
			ResourceNames: getInvolvedSecretNames(opts),
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
				opts.Cluster.Name,
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
				opts.Cluster.Name,
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
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"databases",
			},
			Verbs: []string{
				"get",
				"update",
				"list",
				"watch",
			},
			ResourceNames: []string{},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"databases/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"publications",
			},
			Verbs: []string{
				"get",
				"update",
				"list",
				"watch",
			},
			ResourceNames: []string{},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"publications/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"subscriptions",
			},
			Verbs: []string{
				"get",
				"update",
				"list",
				"watch",
			},
			ResourceNames: []string{},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"subscriptions/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"failoverquorums",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			ResourceNames: []string{
				opts.Cluster.Name,
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"failoverquorums/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
				"watch",
			},
			ResourceNames: []string{
				opts.Cluster.Name,
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"databaseroles",
			},
			Verbs: []string{
				"get",
				"update",
				"list",
				"watch",
			},
			ResourceNames: []string{},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"databaseroles/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
				"watch",
			},
		},
	}

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: opts.Cluster.Namespace,
			Name:      opts.Cluster.Name,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Rules: rules,
	}
}

func getInvolvedSecretNames(opts RoleOptions) []string {
	involvedSecretNames := []string{
		opts.Cluster.GetReplicationSecretName(),
		opts.Cluster.GetClientCASecretName(),
		opts.Cluster.GetServerCASecretName(),
		opts.Cluster.GetServerTLSSecretName(),
		opts.Cluster.GetApplicationSecretName(),
		opts.Cluster.GetSuperuserSecretName(),
		opts.Cluster.GetLDAPSecretName(),
	}

	if opts.Cluster.Spec.Monitoring != nil {
		for _, secretName := range opts.Cluster.Spec.Monitoring.CustomQueriesSecret {
			involvedSecretNames = append(involvedSecretNames, secretName.Name)
		}
	}

	involvedSecretNames = append(involvedSecretNames, backupSecrets(opts.Cluster, opts.BackupOrigin)...)
	involvedSecretNames = append(involvedSecretNames, externalClusterSecrets(opts.Cluster)...)
	involvedSecretNames = append(involvedSecretNames, managedRolesSecrets(opts.Cluster)...)
	involvedSecretNames = append(involvedSecretNames, customResourceRolesSecrets(opts.Roles)...)

	return cleanupResourceList(involvedSecretNames)
}

func getInvolvedConfigMapNames(cluster *apiv1.Cluster) []string {
	involvedConfigMapNames := []string{
		cluster.Name,
	}

	if cluster.Spec.Monitoring != nil {
		// If custom queries are used, the instance manager need privileges to read those
		// entries
		for _, configMapName := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
			involvedConfigMapNames = append(involvedConfigMapNames, configMapName.Name)
		}
	}

	return cleanupResourceList(involvedConfigMapNames)
}

// cleanupResourceList returns a new list with the same elements as resourceList, where
// the empty and duplicate entries have been removed
func cleanupResourceList(resourceList []string) []string {
	result := stringset.From(resourceList).ToSortedList()
	return slices.DeleteFunc(result, func(s string) bool {
		return len(s) == 0
	})
}

func externalClusterSecrets(cluster *apiv1.Cluster) []string {
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
				s3CredentialsSecrets(barmanObjStore.AWS)...)
			result = append(
				result,
				azureCredentialsSecrets(barmanObjStore.Azure)...)
			result = append(
				result,
				googleCredentialsSecrets(barmanObjStore.Google)...)
			if barmanObjStore.EndpointCA != nil {
				result = append(result, barmanObjStore.EndpointCA.Name)
			}
		}
	}

	return result
}

func backupSecrets(cluster *apiv1.Cluster, backupOrigin *apiv1.Backup) []string {
	var result []string

	// Secrets needed to access S3 and Azure
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		result = append(
			result,
			s3CredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.AWS)...)
		result = append(
			result,
			azureCredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.Azure)...)
		result = append(
			result,
			googleCredentialsSecrets(cluster.Spec.Backup.BarmanObjectStore.Google)...)
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
			s3CredentialsSecrets(backupOrigin.Status.AWS)...)
		result = append(
			result,
			azureCredentialsSecrets(backupOrigin.Status.Azure)...)
		result = append(
			result,
			googleCredentialsSecrets(backupOrigin.Status.Google)...)
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

	if s3Credentials.RegionReference != nil {
		secrets = append(secrets, s3Credentials.RegionReference.Name)
	}

	if s3Credentials.SessionToken != nil {
		secrets = append(secrets, s3Credentials.SessionToken.Name)
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

func managedRolesSecrets(cluster *apiv1.Cluster) []string {
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

func customResourceRolesSecrets(roles []apiv1.DatabaseRole) []string {
	result := make([]string, 0, len(roles))

	for _, r := range roles {
		if secretName := crdRoleSecretName(r); secretName != "" {
			result = append(result, secretName)
		}
	}

	return result
}

func crdRoleSecretName(role apiv1.DatabaseRole) string {
	if role.Spec.DisablePassword || role.Spec.PasswordSecret == nil {
		return ""
	}
	return role.Spec.GetRoleSecretName()
}
