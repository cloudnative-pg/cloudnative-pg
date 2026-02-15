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

// CreateRole create a role with the permissions needed by the instance manager
func CreateRole(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) rbacv1.Role {
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
			ResourceNames: getInvolvedConfigMapNames(cluster),
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
			ResourceNames: getInvolvedSecretNames(cluster, backupOrigin),
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
				cluster.Name,
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
				cluster.Name,
			},
		},
	}

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Rules: rules,
	}
}

// GetCrossNamespaceDatabaseRoleName returns the name of the ClusterRole
// used for cross-namespace Database access
func GetCrossNamespaceDatabaseRoleName(cluster apiv1.Cluster) string {
	return "cnpg-" + cluster.Namespace + "-" + cluster.Name + "-cross-ns-db"
}

// CreateCrossNamespaceDatabaseRole creates a ClusterRole with the permissions
// needed by the instance manager to manage Database resources from any namespace
func CreateCrossNamespaceDatabaseRole(cluster apiv1.Cluster) rbacv1.ClusterRole {
	return rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetCrossNamespaceDatabaseRoleName(cluster),
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.ClusterNamespaceLabelName:       cluster.Namespace,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"postgresql.cnpg.io"},
				Resources: []string{"databases"},
				Verbs:     []string{"get", "update", "list", "watch"},
			},
			{
				APIGroups: []string{"postgresql.cnpg.io"},
				Resources: []string{"databases/status"},
				Verbs:     []string{"get", "patch", "update"},
			},
		},
	}
}

// CreateCrossNamespaceDatabaseRoleBinding creates a ClusterRoleBinding that binds
// the cross-namespace Database ClusterRole to the cluster's ServiceAccount
func CreateCrossNamespaceDatabaseRoleBinding(cluster apiv1.Cluster) rbacv1.ClusterRoleBinding {
	return rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetCrossNamespaceDatabaseRoleName(cluster),
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.ClusterNamespaceLabelName:       cluster.Namespace,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     GetCrossNamespaceDatabaseRoleName(cluster),
		},
	}
}

func getInvolvedSecretNames(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) []string {
	involvedSecretNames := []string{
		cluster.GetReplicationSecretName(),
		cluster.GetClientCASecretName(),
		cluster.GetServerCASecretName(),
		cluster.GetServerTLSSecretName(),
		cluster.GetApplicationSecretName(),
		cluster.GetSuperuserSecretName(),
		cluster.GetLDAPSecretName(),
	}

	if cluster.Spec.Monitoring != nil {
		for _, secretName := range cluster.Spec.Monitoring.CustomQueriesSecret {
			involvedSecretNames = append(involvedSecretNames, secretName.Name)
		}
	}

	involvedSecretNames = append(involvedSecretNames, backupSecrets(cluster, backupOrigin)...)
	involvedSecretNames = append(involvedSecretNames, externalClusterSecrets(cluster)...)
	involvedSecretNames = append(involvedSecretNames, managedRolesSecrets(cluster)...)

	return cleanupResourceList(involvedSecretNames)
}

func getInvolvedConfigMapNames(cluster apiv1.Cluster) []string {
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

func backupSecrets(cluster apiv1.Cluster, backupOrigin *apiv1.Backup) []string {
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
