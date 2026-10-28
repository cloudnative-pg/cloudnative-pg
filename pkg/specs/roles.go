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
	"crypto/sha256"
	"encoding/hex"
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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
				"coordination.k8s.io",
			},
			Resources: []string{
				"leases",
			},
			Verbs: []string{
				"get",
				"update",
			},
			ResourceNames: []string{
				opts.Cluster.Name,
			},
		},
		{
			APIGroups: []string{
				apiv1.SchemeGroupVersion.Group,
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
				apiv1.SchemeGroupVersion.Group,
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

// GetCrossNamespaceRoleName returns the name of the ClusterRole and of the
// ClusterRoleBinding used by the cluster to access the resources referring to
// it from other namespaces
func GetCrossNamespaceRoleName(cluster apiv1.Cluster) string {
	return GetCrossNamespaceRoleNameFor(cluster.Namespace, cluster.Name)
}

// GetCrossNamespaceRoleNameFor returns the name of the ClusterRole and of the
// ClusterRoleBinding used to access the resources from other namespaces by the
// cluster having the passed namespace and name. It allows computing the name
// when the Cluster object is no longer available, such as while reconciling
// its deletion.
//
// The objects are cluster-scoped, so their name must identify the namespace
// and the name of the cluster. Both may contain dashes, so joining them would
// be ambiguous: the name embeds a hash of the namespaced name instead, whose
// separator cannot appear in either part.
func GetCrossNamespaceRoleNameFor(namespace, name string) string {
	sum := sha256.Sum256([]byte(namespace + "/" + name))
	return "cnpg-cross-ns-" + hex.EncodeToString(sum[:])[:crossNamespaceRoleHashLength]
}

// crossNamespaceRoleHashLength is the number of hexadecimal digits of the hash
// of the namespaced name of the cluster used in the name of its cross-namespace
// ClusterRole: 64 bits make a collision between two clusters negligible
const crossNamespaceRoleHashLength = 16

// crossNamespaceManagedResources are the resources the instance manager
// reconciles, which may refer to its cluster from other namespaces
var crossNamespaceManagedResources = []string{
	"databases",
	"databaseroles",
	"publications",
	"subscriptions",
}

// CreateCrossNamespaceRole creates a ClusterRole with the permissions needed
// by the instance manager to reconcile the Database, DatabaseRole,
// Publication and Subscription resources from any namespace
func CreateCrossNamespaceRole(cluster apiv1.Cluster) rbacv1.ClusterRole {
	statusResources := make([]string, 0, len(crossNamespaceManagedResources))
	for _, resource := range crossNamespaceManagedResources {
		statusResources = append(statusResources, resource+"/status")
	}

	return rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   GetCrossNamespaceRoleName(cluster),
			Labels: crossNamespaceRBACLabels(cluster),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{apiv1.SchemeGroupVersion.Group},
				Resources: slices.Clone(crossNamespaceManagedResources),
				Verbs:     []string{"get", "update", "list", "watch"},
			},
			{
				APIGroups: []string{apiv1.SchemeGroupVersion.Group},
				Resources: statusResources,
				Verbs:     []string{"get", "patch", "update"},
			},
		},
	}
}

// crossNamespaceRBACLabels returns the labels identifying the cluster owning
// one of the RBAC objects granting access to other namespaces: those objects
// cannot have an owner reference to the cluster, which lives in a namespace
func crossNamespaceRBACLabels(cluster apiv1.Cluster) map[string]string {
	return CrossNamespaceRBACLabelsFor(cluster.Namespace, cluster.Name)
}

// CrossNamespaceRBACLabelsFor returns the labels identifying the RBAC objects
// granting access to other namespaces to the cluster having the passed
// namespace and name. It allows finding them when the Cluster object is no
// longer available.
func CrossNamespaceRBACLabelsFor(namespace, name string) map[string]string {
	return map[string]string{
		utils.ClusterLabelName:                name,
		utils.ClusterNamespaceLabelName:       namespace,
		utils.KubernetesAppManagedByLabelName: utils.ManagerName,
	}
}

// IsCrossNamespaceRBACOwnedBy tells whether the labels of an RBAC object
// identify it as created by the operator for the cluster having the passed
// namespace and name
func IsCrossNamespaceRBACOwnedBy(labels map[string]string, namespace, name string) bool {
	return labels[utils.ClusterLabelName] == name &&
		labels[utils.ClusterNamespaceLabelName] == namespace &&
		labels[utils.KubernetesAppManagedByLabelName] == utils.ManagerName
}

// CreateCrossNamespaceRoleBinding creates a ClusterRoleBinding that binds
// the cross-namespace ClusterRole to the cluster's ServiceAccount
func CreateCrossNamespaceRoleBinding(cluster apiv1.Cluster) rbacv1.ClusterRoleBinding {
	return rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   GetCrossNamespaceRoleName(cluster),
			Labels: crossNamespaceRBACLabels(cluster),
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
			Name:     GetCrossNamespaceRoleName(cluster),
		},
	}
}

// GetCrossNamespaceSecretNames returns the names of the password Secrets the
// instance manager must read to apply the passwords of the passed
// DatabaseRoles, sorted and without duplicates
func GetCrossNamespaceSecretNames(roles []apiv1.DatabaseRole) []string {
	return cleanupResourceList(customResourceRolesSecrets(roles))
}

// CreateCrossNamespaceSecretsRole creates the Role allowing the instance
// manager to read, in the passed namespace, the password Secrets of the
// DatabaseRoles of the cluster living there.
//
// A ClusterRole cannot be used here, as its resource names would match the
// Secrets of every namespace. The caller must not create the Role when
// there is no Secret to read: a rule without resource names would grant
// access to every Secret of the namespace.
func CreateCrossNamespaceSecretsRole(
	cluster apiv1.Cluster,
	namespace string,
	secretNames []string,
) rbacv1.Role {
	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetCrossNamespaceRoleName(cluster),
			Namespace: namespace,
			Labels:    crossNamespaceRBACLabels(cluster),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				Verbs:         []string{"get", "watch"},
				ResourceNames: slices.Clone(secretNames),
			},
		},
	}
}

// CreateCrossNamespaceSecretsRoleBinding creates the RoleBinding granting,
// in the passed namespace, the Role created by CreateCrossNamespaceSecretsRole
// to the ServiceAccount of the cluster
func CreateCrossNamespaceSecretsRoleBinding(cluster apiv1.Cluster, namespace string) rbacv1.RoleBinding {
	return rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetCrossNamespaceRoleName(cluster),
			Namespace: namespace,
			Labels:    crossNamespaceRBACLabels(cluster),
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
			Kind:     "Role",
			Name:     GetCrossNamespaceRoleName(cluster),
		},
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

	if bootstrap := opts.Cluster.Spec.Bootstrap; bootstrap != nil && bootstrap.Recovery != nil &&
		bootstrap.Recovery.Backup != nil && bootstrap.Recovery.Backup.EndpointCA != nil {
		involvedSecretNames = append(involvedSecretNames, bootstrap.Recovery.Backup.EndpointCA.Name)
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
		if backupOrigin.Status.EndpointCA != nil {
			result = append(result, backupOrigin.Status.EndpointCA.Name)
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

	for i := range roles {
		if secretName := crdRoleSecretName(&roles[i]); secretName != "" {
			result = append(result, secretName)
		}
	}

	return result
}

// crdRoleSecretName returns the Secret the instance manager must read to apply
// the password of a DatabaseRole, be it supplied through `passwordSecret` or
// generated by the operator. Without it in the RBAC rule the instance manager
// cannot even create the role, since it cannot read the password to give it.
func crdRoleSecretName(role *apiv1.DatabaseRole) string {
	if role.Spec.DisablePassword {
		return ""
	}
	return role.GetPasswordSecretName()
}
