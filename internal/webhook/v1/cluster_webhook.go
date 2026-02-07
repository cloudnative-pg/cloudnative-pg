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

package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	barmanWebhooks "github.com/cloudnative-pg/barman-cloud/pkg/api/webhooks"
	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/cloudnative-pg/machinery/pkg/types"
	jsonpatch "github.com/evanphx/json-patch/v5"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutil "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const sharedBuffersParameter = "shared_buffers"

// clusterLog is for logging in this package.
var clusterLog = log.WithName("cluster-resource").WithValues("version", "v1")

// SetupClusterWebhookWithManager registers the webhook for Cluster in the manager.
func SetupClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.Cluster{}).
		WithValidator(newBypassableValidator[*apiv1.Cluster](&ClusterCustomValidator{})).
		WithDefaulter(&ClusterCustomDefaulter{}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,verbs=create;update,versions=v1,name=mcluster.cnpg.io,sideEffects=None

// ClusterCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Cluster when those are created or updated.
type ClusterCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Cluster.
func (d *ClusterCustomDefaulter) Default(_ context.Context, cluster *apiv1.Cluster) error {
	clusterLog.Info("Defaulting for Cluster", "name", cluster.GetName(), "namespace", cluster.GetNamespace())

	cluster.Default()

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,versions=v1,name=vcluster.cnpg.io,sideEffects=None

// ClusterCustomValidator struct is responsible for validating the Cluster resource
// when it is created, updated, or deleted.
type ClusterCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (v *ClusterCustomValidator) ValidateCreate(_ context.Context, cluster *apiv1.Cluster) (admission.Warnings, error) {
	clusterLog.Info("Validation for Cluster upon creation", "name", cluster.GetName(), "namespace",
		cluster.GetNamespace())

	allErrs := v.validate(cluster)
	allWarnings := v.getAdmissionWarnings(cluster)

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Cluster"},
		cluster.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (v *ClusterCustomValidator) ValidateUpdate(
	_ context.Context,
	oldCluster *apiv1.Cluster, cluster *apiv1.Cluster,
) (admission.Warnings, error) {
	clusterLog.Info("Validation for Cluster upon update", "name", cluster.GetName(), "namespace",
		cluster.GetNamespace())

	// applying defaults before validating updates to set any new default
	oldCluster.SetDefaults()

	allErrs := append(
		v.validate(cluster),
		v.validateClusterChanges(cluster, oldCluster)...,
	)
	allWarnings := v.getAdmissionWarnings(cluster)

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return allWarnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.cnpg.io", Kind: "Cluster"},
		cluster.Name, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (v *ClusterCustomValidator) ValidateDelete(_ context.Context, cluster *apiv1.Cluster) (admission.Warnings, error) {
	clusterLog.Info("Validation for Cluster upon deletion", "name", cluster.GetName(), "namespace",
		cluster.GetNamespace())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validateCluster groups the validation logic for clusters returning a list of all encountered errors
func (v *ClusterCustomValidator) validate(r *apiv1.Cluster) (allErrs field.ErrorList) {
	type validationFunc func(*apiv1.Cluster) field.ErrorList
	validations := []validationFunc{
		v.validateInitDB,
		v.validateRecoveryApplicationDatabase,
		v.validatePgBaseBackupApplicationDatabase,
		v.validateImport,
		v.validateSuperuserSecret,
		v.validateCerts,
		v.validateBootstrapMethod,
		v.validateImageName,
		v.validateImagePullPolicy,
		v.validateRecoveryTarget,
		v.validatePrimaryUpdateStrategy,
		v.validateMinSyncReplicas,
		v.validateMaxSyncReplicas,
		v.validateStorageSize,
		v.validateWalStorageSize,
		v.validateEphemeralVolumeSource,
		v.validateTablespaceStorageSize,
		v.validateName,
		v.validateTablespaceNames,
		v.validateBootstrapPgBaseBackupSource,
		v.validateTablespaceBackupSnapshot,
		v.validateBootstrapRecoverySource,
		v.validateBootstrapRecoveryDataSource,
		v.validateExternalClusters,
		v.validateTolerations,
		v.validateAntiAffinity,
		v.validateReplicaMode,
		v.validateBackupConfiguration,
		v.validateRetentionPolicy,
		v.validateConfiguration,
		v.validateSynchronousReplicaConfiguration,
		v.validateFailoverQuorumAlphaAnnotation,
		v.validateFailoverQuorum,
		v.validateLDAP,
		v.validateReplicationSlots,
		v.validateSynchronizeLogicalDecoding,
		v.validateEnv,
		v.validateManagedServices,
		v.validateManagedRoles,
		v.validateManagedExtensions,
		v.validateResources,
		v.validateHibernationAnnotation,
		v.validatePodPatchAnnotation,
		v.validatePromotionToken,
		v.validatePluginConfiguration,
		v.validateLivenessPingerProbe,
		v.validateExtensions,
	}

	for _, validate := range validations {
		allErrs = append(allErrs, validate(r)...)
	}

	return allErrs
}

// validateClusterChanges groups the validation logic for cluster changes checking the differences between
// the previous version and the new one of the cluster, returning a list of all encountered errors
func (v *ClusterCustomValidator) validateClusterChanges(r, old *apiv1.Cluster) (allErrs field.ErrorList) {
	if old == nil {
		clusterLog.Info("Received invalid old object, skipping old object validation",
			"old", old)
		return nil
	}
	type validationFunc func(*apiv1.Cluster, *apiv1.Cluster) field.ErrorList
	validations := []validationFunc{
		v.validateImageChange,
		v.validateConfigurationChange,
		v.validateStorageChange,
		v.validateWalStorageChange,
		v.validateTablespacesChange,
		v.validateUnixPermissionIdentifierChange,
		v.validateReplicationSlotsChange,
		v.validateWALLevelChange,
		v.validateReplicaClusterChange,
	}
	for _, validate := range validations {
		allErrs = append(allErrs, validate(r, old)...)
	}

	return allErrs
}

// validateLDAP validates the ldap postgres configuration
func (v *ClusterCustomValidator) validateLDAP(r *apiv1.Cluster) field.ErrorList {
	// No validating if not specified
	if r.Spec.PostgresConfiguration.LDAP == nil {
		return nil
	}
	var result field.ErrorList

	ldapConfig := r.Spec.PostgresConfiguration.LDAP
	if ldapConfig.Server == "" {
		result = append(result,
			field.Invalid(field.NewPath("spec", "postgresql", "ldap"),
				ldapConfig.Server,
				"ldap server cannot be empty if any other ldap parameters are specified"))
	}

	if ldapConfig.BindSearchAuth != nil && ldapConfig.BindAsAuth != nil {
		result = append(
			result,
			field.Invalid(field.NewPath("spec", "postgresql", "ldap"),
				"bindAsAuth or bindSearchAuth",
				"only bind+search or bind method can be specified"))
	}

	return result
}

// validateEnv validate the environment variables settings proposed by the user
func (v *ClusterCustomValidator) validateEnv(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	for i := range r.Spec.Env {
		if isReservedEnvironmentVariable(r.Spec.Env[i].Name) {
			result = append(
				result,
				field.Invalid(field.NewPath("spec", "postgresql", "env").Index(i).Child("name"),
					r.Spec.Env[i].Name,
					"the usage of this environment variable is reserved for the operator",
				))
		}
	}

	return result
}

// isReservedEnvironmentVariable detects if a certain environment variable
// is reserved for the usage of the operator
func isReservedEnvironmentVariable(name string) bool {
	name = strings.ToUpper(name)

	switch {
	case strings.HasPrefix(name, "CNPG_"):
		return true

	case strings.HasPrefix(name, "PG"):
		return true

	case name == "POD_NAME":
		return true

	case name == "NAMESPACE":
		return true

	case name == "CLUSTER_NAME":
		return true
	}

	return false
}

// validateInitDB validate the bootstrapping options when initdb
// method is used
func (v *ClusterCustomValidator) validateInitDB(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	if r.Spec.Bootstrap.InitDB == nil {
		return result
	}

	// If you specify the database name, then you need also to specify the
	// owner user and vice-versa
	initDBOptions := r.Spec.Bootstrap.InitDB
	result = v.validateApplicationDatabase(initDBOptions.Database, initDBOptions.Owner,
		"initdb")

	if initDBOptions.WalSegmentSize != 0 && !utils.IsPowerOfTwo(initDBOptions.WalSegmentSize) {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "walSegmentSize"),
				initDBOptions.WalSegmentSize,
				"WAL segment size must be a power of 2"))
	}

	if initDBOptions.PostInitApplicationSQLRefs != nil {
		for _, item := range initDBOptions.PostInitApplicationSQLRefs.SecretRefs {
			if item.Name == "" || item.Key == "" {
				result = append(
					result,
					field.Invalid(
						field.NewPath("spec", "bootstrap", "initdb", "postInitApplicationSQLRefs", "secretRefs"),
						item,
						"key and name must be specified"))
			}
		}

		for _, item := range initDBOptions.PostInitApplicationSQLRefs.ConfigMapRefs {
			if item.Name == "" || item.Key == "" {
				result = append(
					result,
					field.Invalid(
						field.NewPath("spec", "bootstrap", "initdb", "postInitApplicationSQLRefs", "configMapRefs"),
						item,
						"key and name must be specified"))
			}
		}
	}

	return result
}

func (v *ClusterCustomValidator) validateImport(r *apiv1.Cluster) field.ErrorList {
	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return nil
	}

	if r.Spec.Bootstrap.InitDB == nil {
		return nil
	}

	importSpec := r.Spec.Bootstrap.InitDB.Import
	if importSpec == nil {
		return nil
	}

	switch importSpec.Type {
	case apiv1.MicroserviceSnapshotType:
		return v.validateMicroservice(importSpec)
	case apiv1.MonolithSnapshotType:
		return v.validateMonolith(importSpec)
	default:
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "type"),
				importSpec.Type,
				"Unrecognized import type"),
		}
	}
}

func (v *ClusterCustomValidator) validateMicroservice(s *apiv1.Import) field.ErrorList {
	var result field.ErrorList

	if len(s.Databases) != 1 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "databases"),
				s.Databases,
				"You need to specify a single database for the `microservice` import type"),
		)
	}

	if len(s.Roles) != 0 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "roles"),
				s.Databases,
				"You cannot specify roles to import for the `microservice` import type"),
		)
	}

	if len(s.Databases) == 1 && strings.Contains(s.Databases[0], "*") {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "databases", "0"),
				s.Databases,
				"You cannot specify any wildcard for the `microservice` import type"),
		)
	}

	return result
}

func (v *ClusterCustomValidator) validateMonolith(s *apiv1.Import) field.ErrorList {
	var result field.ErrorList

	if len(s.Databases) < 1 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "databases"),
				s.Databases,
				"You need to specify at least a database for the `monolith` import type"),
		)
	}

	if len(s.Databases) > 1 && slices.Contains(s.Databases, "*") {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "databases"),
				s.Databases,
				"Wildcard import cannot be used along other database names"),
		)
	}

	if len(s.Roles) > 1 && slices.Contains(s.Roles, "*") {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "roles"),
				s.Databases,
				"Wildcard import cannot be used along other role names"),
		)
	}

	if len(s.PostImportApplicationSQL) > 0 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "postImportApplicationSQL"),
				s.PostImportApplicationSQL,
				"postImportApplicationSQL is not allowed for the `monolith` import type"),
		)
	}

	return result
}

// validateRecovery validate the bootstrapping options when Recovery
// method is used
func (v *ClusterCustomValidator) validateRecoveryApplicationDatabase(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	if r.Spec.Bootstrap.Recovery == nil {
		return result
	}

	recoveryOptions := r.Spec.Bootstrap.Recovery
	return v.validateApplicationDatabase(recoveryOptions.Database, recoveryOptions.Owner, "recovery")
}

// validatePgBaseBackup validate the bootstrapping options when pg_basebackup
// method is used
func (v *ClusterCustomValidator) validatePgBaseBackupApplicationDatabase(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	if r.Spec.Bootstrap.PgBaseBackup == nil {
		return result
	}

	pgBaseBackupOptions := r.Spec.Bootstrap.PgBaseBackup
	return v.validateApplicationDatabase(pgBaseBackupOptions.Database, pgBaseBackupOptions.Owner,
		"pg_basebackup")
}

// validateApplicationDatabase validate the configuration for application database
func (v *ClusterCustomValidator) validateApplicationDatabase(
	database string,
	owner string,
	command string,
) field.ErrorList {
	var result field.ErrorList
	// If you specify the database name, then you need also to specify the
	// owner user and vice versa
	if database != "" && owner == "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", command, "owner"),
				"",
				"You need to specify the database owner user"))
	}
	if database == "" && owner != "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", command, "database"),
				"",
				"You need to specify the database name"))
	}
	return result
}

// validateCerts validate all the provided certs
func (v *ClusterCustomValidator) validateCerts(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	certificates := r.Spec.Certificates

	if certificates == nil {
		return result
	}

	if certificates.ServerTLSSecret != "" {
		// Currently names are not validated, maybe add this check in future
		if len(certificates.ServerAltDNSNames) != 0 {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "certificates", "serveraltdnsnames"),
					fmt.Sprintf("%v", certificates.ServerAltDNSNames),
					"Server alternative DNS names can't be defined when server TLS secret is provided"))
		}

		// With ServerTLSSecret not empty you must provide the ServerCASecret
		if certificates.ServerCASecret == "" {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "certificates", "servercasecret"),
					"",
					"Server CA secret can't be empty when server TLS secret is provided"))
		}
	}

	// If you provide the ReplicationTLSSecret we must provide the ClientCaSecret
	if certificates.ReplicationTLSSecret != "" && certificates.ClientCASecret == "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "certificates", "clientcasecret"),
				"",
				"Client CA secret can't be empty when client replication secret is provided"))
	}

	return result
}

// ValidateSuperuserSecret validate super user secret value
func (v *ClusterCustomValidator) validateSuperuserSecret(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// If empty, we're ok!
	if r.Spec.SuperuserSecret == nil {
		return result
	}

	// We check that we have a valid name and not empty
	if r.Spec.SuperuserSecret.Name == "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "superusersecret", "name"),
				"",
				"Super user secret name can't be empty"))
	}

	return result
}

// validateBootstrapMethod is used to ensure we have only one
// bootstrap methods active
func (v *ClusterCustomValidator) validateBootstrapMethod(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	bootstrapMethods := 0
	if r.Spec.Bootstrap.InitDB != nil {
		bootstrapMethods++
	}
	if r.Spec.Bootstrap.Recovery != nil {
		bootstrapMethods++
	}
	if r.Spec.Bootstrap.PgBaseBackup != nil {
		bootstrapMethods++
	}

	if bootstrapMethods > 1 {
		result = append(
			result,
			field.Forbidden(
				field.NewPath("spec", "bootstrap"),
				"Only one bootstrap method can be specified at a time"))
	}

	return result
}

// validateBootstrapPgBaseBackupSource is used to ensure that the source
// server is correctly defined
func (v *ClusterCustomValidator) validateBootstrapPgBaseBackupSource(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// This validation is only applicable for physical backup
	// based bootstrap
	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.PgBaseBackup == nil {
		return result
	}

	_, found := r.ExternalCluster(r.Spec.Bootstrap.PgBaseBackup.Source)
	if !found {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "pg_basebackup", "source"),
				r.Spec.Bootstrap.PgBaseBackup.Source,
				fmt.Sprintf("External cluster %v not found", r.Spec.Bootstrap.PgBaseBackup.Source)))
	}

	return result
}

// validateBootstrapRecoverySource is used to ensure that the source
// server is correctly defined
func (v *ClusterCustomValidator) validateBootstrapRecoverySource(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// This validation is only applicable for recovery based bootstrap
	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.Recovery == nil || r.Spec.Bootstrap.Recovery.Source == "" {
		return result
	}

	externalCluster, found := r.ExternalCluster(r.Spec.Bootstrap.Recovery.Source)

	// Ensure the existence of the external cluster
	if !found {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "source"),
				r.Spec.Bootstrap.Recovery.Source,
				fmt.Sprintf("External cluster %v not found", r.Spec.Bootstrap.Recovery.Source)))
	}

	// Ensure the external cluster definition has enough information
	// to be used to recover a data directory
	if externalCluster.BarmanObjectStore == nil && externalCluster.PluginConfiguration == nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "source"),
				r.Spec.Bootstrap.Recovery.Source,
				fmt.Sprintf("External cluster %v cannot be used for recovery: "+
					"both Barman and CNPG-i plugin configurations are missing", r.Spec.Bootstrap.Recovery.Source)))
	}

	return result
}

// validateBootstrapRecoveryDataSource is used to ensure that the data
// source is correctly defined
func (v *ClusterCustomValidator) validateBootstrapRecoveryDataSource(r *apiv1.Cluster) field.ErrorList {
	// This validation is only applicable for datasource-based recovery based bootstrap
	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.Recovery == nil || r.Spec.Bootstrap.Recovery.VolumeSnapshots == nil {
		return nil
	}

	recoveryPath := field.NewPath("spec", "bootstrap", "recovery")
	recoverySection := r.Spec.Bootstrap.Recovery
	if recoverySection.Backup != nil {
		return field.ErrorList{
			field.Invalid(
				recoveryPath.Child("backup"),
				r.Spec.Bootstrap.Recovery.Backup,
				"Recovery from dataSource is not compatible with other types of recovery"),
		}
	}

	if recoverySection.RecoveryTarget != nil && recoverySection.RecoveryTarget.BackupID != "" {
		return field.ErrorList{
			field.Invalid(
				recoveryPath.Child("recoveryTarget", "backupID"),
				r.Spec.Bootstrap.Recovery.RecoveryTarget.BackupID,
				"Cannot specify a backupID when recovering using a DataSource"),
		}
	}

	result := validateVolumeSnapshotSource(recoverySection.VolumeSnapshots.Storage, recoveryPath.Child("storage"))

	if recoverySection.VolumeSnapshots.WalStorage != nil && r.Spec.WalStorage == nil {
		walStoragePath := recoveryPath.Child("dataSource", "walStorage")
		result = append(
			result,
			field.Invalid(
				walStoragePath,
				r.Spec.Bootstrap.Recovery.VolumeSnapshots.WalStorage,
				"A WAL storage configuration is required when recovering using a DataSource for WALs"))
		result = append(
			result,
			validateVolumeSnapshotSource(
				*recoverySection.VolumeSnapshots.WalStorage, walStoragePath)...)
	}

	if recoverySection.VolumeSnapshots.WalStorage != nil {
		result = append(
			result,
			validateVolumeSnapshotSource(
				*recoverySection.VolumeSnapshots.WalStorage,
				recoveryPath.Child("dataSource", "walStorage"))...)
	}

	return result
}

// validateVolumeSnapshotSource validates a source of a recovery snapshot.
// The supported resources are VolumeSnapshots and PersistentVolumeClaim
func validateVolumeSnapshotSource(
	value corev1.TypedLocalObjectReference,
	path *field.Path,
) field.ErrorList {
	apiGroup := ""
	if value.APIGroup != nil {
		apiGroup = *value.APIGroup
	}

	switch {
	case apiGroup == volumesnapshotv1.GroupName && value.Kind == "VolumeSnapshot":
	case apiGroup == corev1.GroupName && value.Kind == "PersistentVolumeClaim":
	default:
		return field.ErrorList{
			field.Invalid(path, value, "Only VolumeSnapshots and PersistentVolumeClaims are supported"),
		}
	}

	return nil
}

// validateImageName validates the image name ensuring we aren't
// using the "latest" tag
func (v *ClusterCustomValidator) validateImageName(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.ImageName == "" {
		// We'll use the default one or the one in the catalog
		return result
	}

	// We have to check if the image has a valid tag
	tag := reference.New(r.Spec.ImageName).Tag
	switch tag {
	case "latest":
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Can't use 'latest' as image tag as we can't detect upgrades"))
	case "":
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Can't use just the image sha as we can't detect upgrades"))
	default:
		_, err := version.FromTag(tag)
		if err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "imageName"),
					r.Spec.ImageName,
					"invalid version tag"))
		}
	}
	return result
}

// validateImagePullPolicy validates the image pull policy,
// ensuring it is one of "Always", "Never" or "IfNotPresent" when defined
func (v *ClusterCustomValidator) validateImagePullPolicy(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	switch r.Spec.ImagePullPolicy {
	case corev1.PullAlways, corev1.PullNever, corev1.PullIfNotPresent, "":
		return result
	default:
		return append(
			result,
			field.Invalid(
				field.NewPath("spec", "imagePullPolicy"),
				r.Spec.ImagePullPolicy,
				fmt.Sprintf("invalid imagePullPolicy, if defined must be one of '%s', '%s' or '%s'",
					corev1.PullAlways, corev1.PullNever, corev1.PullIfNotPresent)))
	}
}

func (v *ClusterCustomValidator) validateResources(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	cpuRequests := r.Spec.Resources.Requests.Cpu()
	cpuLimits := r.Spec.Resources.Limits.Cpu()
	if !cpuRequests.IsZero() && !cpuLimits.IsZero() {
		cpuRequestGtThanLimit := cpuRequests.Cmp(*cpuLimits) > 0
		if cpuRequestGtThanLimit {
			result = append(result, field.Invalid(
				field.NewPath("spec", "resources", "requests", "cpu"),
				cpuRequests.String(),
				"CPU request is greater than the limit",
			))
		}
	}

	memoryRequests := r.Spec.Resources.Requests.Memory()
	memoryLimits := r.Spec.Resources.Limits.Memory()
	if !memoryRequests.IsZero() && !memoryLimits.IsZero() {
		memoryRequestGtThanLimit := memoryRequests.Cmp(*memoryLimits) > 0
		if memoryRequestGtThanLimit {
			result = append(result, field.Invalid(
				field.NewPath("spec", "resources", "requests", "memory"),
				memoryRequests.String(),
				"Memory request is greater than the limit",
			))
		}
	}

	hugePages, hugePagesErrors := validateHugePagesResources(r)
	result = append(result, hugePagesErrors...)
	if cpuRequests.IsZero() && cpuLimits.IsZero() && memoryRequests.IsZero() && memoryLimits.IsZero() &&
		len(hugePages) > 0 {
		result = append(result, field.Forbidden(
			field.NewPath("spec", "resources"),
			"HugePages require cpu or memory",
		))
	}

	rawSharedBuffer := r.Spec.PostgresConfiguration.Parameters[sharedBuffersParameter]
	if rawSharedBuffer != "" {
		if sharedBuffers, err := parsePostgresQuantityValue(rawSharedBuffer); err == nil {
			if !hasEnoughMemoryForSharedBuffers(sharedBuffers, memoryRequests, hugePages) {
				result = append(result, field.Invalid(
					field.NewPath("spec", "resources", "requests"),
					memoryRequests.String(),
					"Memory request is lower than PostgreSQL `shared_buffers` value",
				))
			}
		}
	}

	ephemeralStorageRequest := r.Spec.Resources.Requests.StorageEphemeral()
	ephemeralStorageLimits := r.Spec.Resources.Limits.StorageEphemeral()
	if !ephemeralStorageRequest.IsZero() && !ephemeralStorageLimits.IsZero() {
		ephemeralStorageRequestGtThanLimit := ephemeralStorageRequest.Cmp(*ephemeralStorageLimits) > 0
		if ephemeralStorageRequestGtThanLimit {
			result = append(result, field.Invalid(
				field.NewPath("spec", "resources", "requests", "storage"),
				ephemeralStorageRequest.String(),
				"Ephemeral storage request is greater than the limit",
			))
		}
	}

	return result
}

func validateHugePagesResources(r *apiv1.Cluster) (map[corev1.ResourceName]resource.Quantity, field.ErrorList) {
	var result field.ErrorList
	hugepages := make(map[corev1.ResourceName]resource.Quantity)
	for name, quantity := range r.Spec.Resources.Limits {
		if strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix) {
			hugepages[name] = quantity
		}
	}
	for name, quantity := range r.Spec.Resources.Requests {
		if strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix) {
			if existingQuantity, exists := hugepages[name]; exists {
				if existingQuantity.Cmp(quantity) != 0 {
					result = append(result, field.Invalid(
						field.NewPath("spec", "resources", "requests", string(name)),
						quantity.String(),
						"HugePages requests must equal the limits",
					))
				}
				continue
			}
			hugepages[name] = quantity
		}
	}
	return hugepages, result
}

func hasEnoughMemoryForSharedBuffers(
	sharedBuffers resource.Quantity,
	memoryRequest *resource.Quantity,
	hugePages map[corev1.ResourceName]resource.Quantity,
) bool {
	if memoryRequest.IsZero() || sharedBuffers.Cmp(*memoryRequest) <= 0 {
		return true
	}

	for _, quantity := range hugePages {
		if sharedBuffers.Cmp(quantity) <= 0 {
			return true
		}
	}

	return false
}

func (v *ClusterCustomValidator) validateSynchronousReplicaConfiguration(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.PostgresConfiguration.Synchronous == nil {
		return nil
	}

	var result field.ErrorList

	cfg := r.Spec.PostgresConfiguration.Synchronous
	if cfg.Number >= (r.Spec.Instances +
		len(cfg.StandbyNamesPost) +
		len(cfg.StandbyNamesPre)) {
		err := field.Invalid(
			field.NewPath("spec", "postgresql", "synchronous"),
			cfg,
			"Invalid synchronous configuration: the number of synchronous replicas must be less than the "+
				"total number of instances and the provided standby names.",
		)
		result = append(result, err)
	}

	return result
}

func (v *ClusterCustomValidator) validateFailoverQuorumAlphaAnnotation(r *apiv1.Cluster) field.ErrorList {
	annotationValue, ok := r.Annotations[utils.FailoverQuorumAnnotationName]
	if !ok {
		return nil
	}

	failoverQuorumActive, err := strconv.ParseBool(annotationValue)
	if err != nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("metadata", "annotations", utils.FailoverQuorumAnnotationName),
				r.Annotations[utils.FailoverQuorumAnnotationName],
				"Invalid failoverQuorum annotation value, expected boolean.",
			),
		}
	}

	if !failoverQuorumActive {
		return nil
	}

	if r.Spec.PostgresConfiguration.Synchronous == nil {
		return field.ErrorList{
			field.Required(
				field.NewPath("spec", "postgresql", "synchronous"),
				"Invalid failoverQuorum configuration: synchronous replication configuration "+
					"is required.",
			),
		}
	}

	return nil
}

func (v *ClusterCustomValidator) validateFailoverQuorum(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if !r.IsFailoverQuorumActive() {
		return nil
	}

	cfg := r.Spec.PostgresConfiguration.Synchronous
	if cfg == nil {
		err := field.Required(
			field.NewPath("spec", "postgresql", "synchronous"),
			"Invalid failoverQuorum configuration: synchronous replication configuration "+
				"is required.",
		)
		result = append(result, err)
		return result
	}

	if cfg.Number <= len(cfg.StandbyNamesPost)+len(cfg.StandbyNamesPre) {
		err := field.Invalid(
			field.NewPath("spec", "postgresql", "synchronous"),
			cfg,
			"Invalid failoverQuorum configuration: spec.postgresql.synchronous.number must be greater than "+
				"the total number of instances in spec.postgresql.synchronous.standbyNamesPre and "+
				"spec.postgresql.synchronous.standbyNamesPost to allow automatic failover.",
		)
		result = append(result, err)
	}

	return result
}

// validateConfiguration determines whether a PostgreSQL configuration is valid
func (v *ClusterCustomValidator) validateConfiguration(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	// We cannot have both old-style synchronous replica configuration
	// and new-style synchronous replica configuration
	haveOldStyleSyncReplicaConfig := r.Spec.PostgresConfiguration.Synchronous != nil
	haveNewStyleSyncReplicaConfig := r.Spec.MinSyncReplicas > 0 || r.Spec.MaxSyncReplicas > 0
	if haveOldStyleSyncReplicaConfig && haveNewStyleSyncReplicaConfig {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "synchronous"),
				r.Spec.PostgresConfiguration.Synchronous,
				"Can't have both legacy synchronous replica configuration and new one"))
	}

	pgMajor, err := r.GetPostgresqlMajorVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}
	if pgMajor < 13 {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Unsupported PostgreSQL version. Versions 13 or newer are supported"))
	}
	info := postgres.ConfigurationInfo{
		Settings:               postgres.CnpgConfigurationSettings,
		MajorVersion:           pgMajor,
		UserSettings:           r.Spec.PostgresConfiguration.Parameters,
		IsReplicaCluster:       r.IsReplica(),
		IsWalArchivingDisabled: utils.IsWalArchivingDisabled(&r.ObjectMeta),
		IsAlterSystemEnabled:   r.Spec.PostgresConfiguration.EnableAlterSystem,
	}
	sanitizedParameters := postgres.CreatePostgresqlConfiguration(info).GetConfigurationParameters()

	for key, value := range r.Spec.PostgresConfiguration.Parameters {
		_, isFixed := postgres.FixedConfigurationParameters[key]
		sanitizedValue, presentInSanitizedConfiguration := sanitizedParameters[key]
		if isFixed && (!presentInSanitizedConfiguration || value != sanitizedValue) {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", key),
					value,
					"Can't set fixed configuration parameter"))
		}
	}

	walLevel := postgres.WalLevelValue(sanitizedParameters[postgres.ParameterWalLevel])
	hasWalLevelRequirement := r.Spec.Instances > 1 || sanitizedParameters[postgres.ParameterArchiveMode] != "off" ||
		r.IsReplica()
	if !walLevel.IsKnownValue() {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", postgres.ParameterWalLevel),
				walLevel,
				fmt.Sprintf("unrecognized `wal_level` value - allowed values: `%s`, `%s`, `%s`",
					postgres.WalLevelValueLogical,
					postgres.WalLevelValueReplica,
					postgres.WalLevelValueMinimal,
				)))
	} else if hasWalLevelRequirement && !walLevel.IsStricterThanMinimal() {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", postgres.ParameterWalLevel),
				walLevel,
				"`wal_level` should be set at `logical` or `replica` when `archive_mode` is `on`, "+
					"'.instances' field is greater than 1, or this is a replica cluster"))
	}

	if walLevel == "minimal" {
		if value, ok := sanitizedParameters[postgres.ParameterMaxWalSenders]; !ok || value != "0" {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", "max_wal_senders"),
					walLevel,
					"`max_wal_senders` should be set at `0` when `wal_level` is `minimal`"))
		}
	}

	if value := r.Spec.PostgresConfiguration.Parameters[sharedBuffersParameter]; value != "" {
		if _, err := parsePostgresQuantityValue(value); err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", sharedBuffersParameter),
					sharedBuffersParameter,
					fmt.Sprintf(
						"Invalid value for configuration parameter %s. More info on accepted values format: "+
							"https://www.postgresql.org/docs/current/config-setting.html#CONFIG-SETTING-NAMES-VALUES",
						sharedBuffersParameter,
					)))
		}
	}

	if _, fieldError := tryParseBooleanPostgresParameter(r, postgres.ParameterHotStandbyFeedback); fieldError != nil {
		result = append(result, fieldError)
	}

	if _, fieldError := tryParseBooleanPostgresParameter(r, postgres.ParameterSyncReplicationSlots); fieldError != nil {
		result = append(result, fieldError)
	}

	walLogHintsActivated, fieldError := tryParseBooleanPostgresParameter(r, postgres.ParameterWalLogHints)
	if fieldError != nil {
		result = append(result, fieldError)
	}
	if walLogHintsActivated != nil && !*walLogHintsActivated && r.Spec.Instances > 1 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", postgres.ParameterWalLogHints),
				r.Spec.PostgresConfiguration.Parameters[postgres.ParameterWalLogHints],
				"`wal_log_hints` must be set to `on` when `instances` > 1"))
	}

	// verify the postgres setting min_wal_size < max_wal_size < volume size
	result = append(result, validateWalSizeConfiguration(
		r.Spec.PostgresConfiguration, r.Spec.WalStorage.GetSizeOrNil())...)

	if err := validateSyncReplicaElectionConstraint(
		r.Spec.PostgresConfiguration.SyncReplicaElectionConstraint,
	); err != nil {
		result = append(result, err)
	}

	return result
}

// tryParseBooleanPostgresParameter attempts to parse a boolean PostgreSQL parameter
// from the cluster specification. If the parameter is not set, it returns nil.
func tryParseBooleanPostgresParameter(r *apiv1.Cluster, parameterName string) (*bool, *field.Error) {
	stringValue, hasParameter := r.Spec.PostgresConfiguration.Parameters[parameterName]
	if !hasParameter {
		return nil, nil
	}

	value, err := postgres.ParsePostgresConfigBoolean(stringValue)
	if err != nil {
		return nil, field.Invalid(
			field.NewPath("spec", "postgresql", "parameters", parameterName),
			stringValue,
			fmt.Sprintf("invalid `%s` value. Must be a postgres boolean", parameterName))
	}
	return &value, nil
}

// validateWalSizeConfiguration verifies that min_wal_size < max_wal_size < wal volume size
func validateWalSizeConfiguration(
	postgresConfig apiv1.PostgresConfiguration, walVolumeSize *resource.Quantity,
) field.ErrorList {
	const (
		minWalSizeKey     = "min_wal_size"
		minWalSizeDefault = "80MB"
		maxWalSizeKey     = "max_wal_size"
		maxWalSizeDefault = "1GB"
	)

	var result field.ErrorList

	minWalSize, hasMinWalSize := postgresConfig.Parameters[minWalSizeKey]
	if minWalSize == "" {
		minWalSize = minWalSizeDefault
		hasMinWalSize = false
	}
	minWalSizeValue, err := parsePostgresQuantityValue(minWalSize)
	if err != nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", minWalSizeKey),
				minWalSize,
				fmt.Sprintf("Invalid value for configuration parameter %s", minWalSizeKey)))
	}

	maxWalSize, hasMaxWalSize := postgresConfig.Parameters[maxWalSizeKey]
	if maxWalSize == "" {
		maxWalSize = maxWalSizeDefault
		hasMaxWalSize = false
	}
	maxWalSizeValue, err := parsePostgresQuantityValue(maxWalSize)
	if err != nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", maxWalSizeKey),
				maxWalSize,
				fmt.Sprintf("Invalid value for configuration parameter %s", maxWalSizeKey)))
	}

	if !minWalSizeValue.IsZero() && !maxWalSizeValue.IsZero() &&
		minWalSizeValue.Cmp(maxWalSizeValue) >= 0 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", minWalSizeKey),
				minWalSize,
				fmt.Sprintf("Invalid vale. Parameter %s (default %s) should be smaller than parameter %s (default %s)",
					minWalSizeKey, minWalSizeDefault, maxWalSizeKey, maxWalSizeDefault)))
	}

	if walVolumeSize == nil {
		return result
	}

	if hasMinWalSize &&
		!minWalSizeValue.IsZero() &&
		minWalSizeValue.Cmp(*walVolumeSize) >= 0 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", minWalSizeKey),
				minWalSize,
				fmt.Sprintf("Invalid value. Parameter %s (default %s) should be smaller than WAL volume size",
					minWalSizeKey, minWalSizeDefault)))
	}

	if hasMaxWalSize &&
		!maxWalSizeValue.IsZero() &&
		maxWalSizeValue.Cmp(*walVolumeSize) >= 0 {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", maxWalSizeKey),
				maxWalSize,
				fmt.Sprintf("Invalid value. Parameter %s (default %s) should be smaller than WAL volume size",
					maxWalSizeKey, maxWalSizeDefault)))
	}

	return result
}

// parsePostgresQuantityValue converts the  sizes in the PostgreSQL configuration
// into kubernetes resource.Quantity values
// Ref: Numeric with Unit @ https://www.postgresql.org/docs/current/config-setting.html#CONFIG-SETTING-NAMES-VALUES
func parsePostgresQuantityValue(value string) (resource.Quantity, error) {
	// If no suffix, default is MB
	if _, err := strconv.Atoi(value); err == nil {
		value += "MB"
	}

	// If there is a suffix it must be "B"
	if value[len(value)-1:] != "B" {
		return resource.Quantity{}, resource.ErrFormatWrong
	}

	// Kubernetes uses Mi rather than MB, Gi rather than GB. Drop the "B"
	value = strings.TrimSuffix(value, "B")

	// Spaces are allowed in postgres between number and unit in Postgres, but not in Kubernetes
	value = strings.ReplaceAll(value, " ", "")

	// Add the 'i' suffix unless it is a bare number (it was 'B' before)
	if _, err := strconv.Atoi(value); err != nil {
		value += "i"

		// 'kB' must translate to 'Ki'
		value = strings.ReplaceAll(value, "ki", "Ki")
	}

	return resource.ParseQuantity(value)
}

// validateConfigurationChange determines whether a PostgreSQL configuration
// change can be applied
func (v *ClusterCustomValidator) validateConfigurationChange(r, old *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if old.Spec.ImageName != r.Spec.ImageName && r.Spec.PrimaryUpdateMethod == apiv1.PrimaryUpdateMethodSwitchover {
		diff := utils.CollectDifferencesFromMaps(old.Spec.PostgresConfiguration.Parameters,
			r.Spec.PostgresConfiguration.Parameters)
		if len(diff) > 0 {
			jsonDiff, _ := json.Marshal(diff)
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "imageName"),
					r.Spec.ImageName,
					fmt.Sprintf("Can't change image name and configuration at the same time when "+
						"`primaryUpdateMethod` is set to `switchover`. "+
						"There are differences in PostgreSQL configuration parameters: %s", jsonDiff)))
			return result
		}
	}

	return result
}

func validateSyncReplicaElectionConstraint(constraints apiv1.SyncReplicaElectionConstraints) *field.Error {
	if !constraints.Enabled {
		return nil
	}
	if len(constraints.NodeLabelsAntiAffinity) > 0 {
		return nil
	}

	return field.Invalid(
		field.NewPath(
			"spec", "postgresql", "syncReplicaElectionConstraint", "nodeLabelsAntiAffinity",
		),
		nil,
		"Can't enable syncReplicaConstraints without passing labels for comparison inside nodeLabelsAntiAffinity",
	)
}

// validateImageChange validate the change from a certain image name
// to a new one.
func (v *ClusterCustomValidator) validateImageChange(r, old *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	var fieldPath *field.Path
	if r.Spec.ImageCatalogRef != nil {
		fieldPath = field.NewPath("spec", "imageCatalogRef", "major")
	} else {
		fieldPath = field.NewPath("spec", "imageName")
	}

	newVersion, err := r.GetPostgresqlMajorVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}

	if old.Status.PGDataImageInfo == nil {
		return result
	}
	oldVersion := old.Status.PGDataImageInfo.MajorVersion

	if oldVersion > newVersion {
		result = append(
			result,
			field.Invalid(
				fieldPath,
				strconv.Itoa(newVersion),
				fmt.Sprintf("can't downgrade from major %v to %v", oldVersion, newVersion)))
	}

	// TODO: Upgrading to versions 14 and 15 would require carrying information around about the collation used.
	//   See https://git.postgresql.org/gitweb/?p=postgresql.git;a=commitdiff;h=9637badd9.
	//   This is not implemented yet, and users should not upgrade to old versions anyway, so we are blocking it.
	if oldVersion < newVersion && newVersion < 16 {
		result = append(
			result,
			field.Invalid(
				fieldPath,
				strconv.Itoa(newVersion),
				"major upgrades are only supported to version 16 or higher"))
	}
	return result
}

// Validate the recovery target to ensure that the mutual exclusivity
// of options is respected and plus validating the format of targetTime
// if specified
func (v *ClusterCustomValidator) validateRecoveryTarget(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.Recovery == nil {
		return nil
	}

	recoveryTarget := r.Spec.Bootstrap.Recovery.RecoveryTarget
	if recoveryTarget == nil {
		return nil
	}

	result := validateTargetExclusiveness(recoveryTarget)

	// validate format of TargetTime
	if recoveryTarget.TargetTime != "" {
		if _, err := types.ParseTargetTime(nil, recoveryTarget.TargetTime); err != nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
				recoveryTarget.TargetTime,
				"The format of TargetTime is invalid"))
		}
	}

	// validate TargetLSN
	if recoveryTarget.TargetLSN != "" {
		if _, err := types.LSN(recoveryTarget.TargetLSN).Parse(); err != nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
				recoveryTarget.TargetLSN,
				"Invalid TargetLSN"))
		}
	}

	// When using a backup catalog, we can identify the backup to be restored
	// only if the PITR is time-based. If the PITR is not time-based, the user
	// need to specify a backup ID.
	// If we use a dataSource, the operator will directly access the backup
	// and a backupID is not needed.

	// validate BackupID is defined when TargetName or TargetXID or TargetImmediate are set
	labelBasedPITR := recoveryTarget.TargetName != "" ||
		recoveryTarget.TargetXID != "" ||
		recoveryTarget.TargetImmediate != nil
	recoveryFromSnapshot := r.Spec.Bootstrap.Recovery.VolumeSnapshots != nil
	if labelBasedPITR && !recoveryFromSnapshot && recoveryTarget.BackupID == "" {
		result = append(result, field.Required(
			field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
			"BackupID is missing"))
	}

	switch recoveryTarget.TargetTLI {
	case "", "latest":
		// Allowed non-numeric values
	default:
		// Everything else must be a valid positive integer
		if tli, err := strconv.Atoi(recoveryTarget.TargetTLI); err != nil || tli < 1 {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget", "targetTLI"),
				recoveryTarget,
				"recovery target timeline can be set to 'latest' or a positive integer"))
		}
	}

	return result
}

func validateTargetExclusiveness(recoveryTarget *apiv1.RecoveryTarget) field.ErrorList {
	targets := 0
	if recoveryTarget.TargetImmediate != nil {
		targets++
	}
	if recoveryTarget.TargetLSN != "" {
		targets++
	}
	if recoveryTarget.TargetName != "" {
		targets++
	}
	if recoveryTarget.TargetXID != "" {
		targets++
	}
	if recoveryTarget.TargetTime != "" {
		targets++
	}

	var result field.ErrorList

	if targets > 1 {
		result = append(result, field.Invalid(
			field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
			recoveryTarget,
			"Recovery target options are mutually exclusive"))
	}
	return result
}

// Validate the update strategy related to the number of required
// instances
func (v *ClusterCustomValidator) validatePrimaryUpdateStrategy(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.PrimaryUpdateStrategy == "" {
		return nil
	}

	var result field.ErrorList

	if r.Spec.PrimaryUpdateStrategy != apiv1.PrimaryUpdateStrategySupervised &&
		r.Spec.PrimaryUpdateStrategy != apiv1.PrimaryUpdateStrategyUnsupervised {
		result = append(result, field.Invalid(
			field.NewPath("spec", "primaryUpdateStrategy"),
			r.Spec.PrimaryUpdateStrategy,
			"primaryUpdateStrategy should be empty, 'supervised' or 'unsupervised'"))
		return result
	}

	if r.Spec.PrimaryUpdateStrategy == apiv1.PrimaryUpdateStrategySupervised && r.Spec.Instances == 1 {
		result = append(result, field.Invalid(
			field.NewPath("spec", "primaryUpdateStrategy"),
			r.Spec.PrimaryUpdateStrategy,
			"supervised update strategy is not allowed for clusters with a single instance"))
		return result
	}

	return nil
}

// Validate the maximum number of synchronous instances
// that should be kept in sync with the primary server
func (v *ClusterCustomValidator) validateMaxSyncReplicas(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.MaxSyncReplicas < 0 {
		result = append(result, field.Invalid(
			field.NewPath("spec", "maxSyncReplicas"),
			r.Spec.MaxSyncReplicas,
			"maxSyncReplicas must be a non negative integer"))
	}

	if r.Spec.MaxSyncReplicas >= r.Spec.Instances {
		result = append(result, field.Invalid(
			field.NewPath("spec", "maxSyncReplicas"),
			r.Spec.MaxSyncReplicas,
			"maxSyncReplicas must be lower than the number of instances"))
	}

	return result
}

// Validate the minimum number of synchronous instances
func (v *ClusterCustomValidator) validateMinSyncReplicas(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.MinSyncReplicas < 0 {
		result = append(result, field.Invalid(
			field.NewPath("spec", "minSyncReplicas"),
			r.Spec.MinSyncReplicas,
			"minSyncReplicas must be a non negative integer"))
	}

	if r.Spec.MinSyncReplicas > r.Spec.MaxSyncReplicas {
		result = append(result, field.Invalid(
			field.NewPath("spec", "minSyncReplicas"),
			r.Spec.MinSyncReplicas,
			"minSyncReplicas cannot be greater than maxSyncReplicas"))
	}

	return result
}

func (v *ClusterCustomValidator) validateStorageSize(r *apiv1.Cluster) field.ErrorList {
	return validateStorageConfigurationSize(*field.NewPath("spec", "storage"), r.Spec.StorageConfiguration)
}

func (v *ClusterCustomValidator) validateWalStorageSize(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.ShouldCreateWalArchiveVolume() {
		result = append(result,
			validateStorageConfigurationSize(*field.NewPath("spec", "walStorage"), *r.Spec.WalStorage)...)
	}

	return result
}

func (v *ClusterCustomValidator) validateEphemeralVolumeSource(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.EphemeralVolumeSource != nil && (r.Spec.EphemeralVolumesSizeLimit != nil &&
		r.Spec.EphemeralVolumesSizeLimit.TemporaryData != nil) {
		result = append(result, field.Duplicate(
			field.NewPath("spec", "ephemeralVolumeSource"),
			"Conflicting settings: provide either ephemeralVolumeSource "+
				"or ephemeralVolumesSizeLimit.TemporaryData, not both.",
		))
	}

	return result
}

func (v *ClusterCustomValidator) validateTablespaceStorageSize(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.Tablespaces == nil {
		return nil
	}

	var result field.ErrorList

	for idx, tablespaceConf := range r.Spec.Tablespaces {
		result = append(result,
			validateStorageConfigurationSize(
				*field.NewPath("spec", "tablespaces").Index(idx),
				tablespaceConf.Storage)...,
		)
	}
	return result
}

func validateStorageConfigurationSize(
	structPath field.Path,
	storageConfiguration apiv1.StorageConfiguration,
) field.ErrorList {
	var result field.ErrorList

	if storageConfiguration.Size != "" {
		if _, err := resource.ParseQuantity(storageConfiguration.Size); err != nil {
			result = append(result, field.Invalid(
				structPath.Child("size"),
				storageConfiguration.Size,
				"Size value isn't valid"))
		}
	}

	if storageConfiguration.Size == "" &&
		(storageConfiguration.PersistentVolumeClaimTemplate == nil ||
			storageConfiguration.PersistentVolumeClaimTemplate.Resources.Requests.Storage().IsZero()) {
		result = append(result, field.Invalid(
			structPath.Child("size"),
			storageConfiguration.Size,
			"Size not configured. Please add it, or a storage request in the pvcTemplate."))
	}

	return result
}

// Validate a change in the storage
func (v *ClusterCustomValidator) validateStorageChange(r, old *apiv1.Cluster) field.ErrorList {
	return validateStorageConfigurationChange(
		field.NewPath("spec", "storage"),
		old.Spec.StorageConfiguration,
		r.Spec.StorageConfiguration,
	)
}

func (v *ClusterCustomValidator) validateWalStorageChange(r, old *apiv1.Cluster) field.ErrorList {
	if old.Spec.WalStorage == nil {
		return nil
	}

	if old.Spec.WalStorage != nil && r.Spec.WalStorage == nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "walStorage"),
				r.Spec.WalStorage,
				"walStorage cannot be disabled once configured"),
		}
	}

	return validateStorageConfigurationChange(
		field.NewPath("spec", "walStorage"),
		*old.Spec.WalStorage,
		*r.Spec.WalStorage,
	)
}

// validateTablespacesChange checks that no tablespaces have been deleted, and that
// no tablespaces have an invalid storage update
func (v *ClusterCustomValidator) validateTablespacesChange(r, old *apiv1.Cluster) field.ErrorList {
	if old.Spec.Tablespaces == nil {
		return nil
	}

	if old.Spec.Tablespaces != nil && r.Spec.Tablespaces == nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "tablespaces"),
				r.Spec.Tablespaces,
				"tablespaces section cannot be deleted once created"),
		}
	}

	var errs field.ErrorList
	for idx, oldConf := range old.Spec.Tablespaces {
		name := oldConf.Name
		if newConf := r.GetTablespaceConfiguration(name); newConf != nil {
			errs = append(errs, validateStorageConfigurationChange(
				field.NewPath("spec", "tablespaces").Index(idx),
				oldConf.Storage,
				newConf.Storage,
			)...)
		} else {
			errs = append(errs,
				field.Invalid(
					field.NewPath("spec", "tablespaces").Index(idx),
					r.Spec.Tablespaces,
					"no tablespace can be deleted once created"))
		}
	}
	return errs
}

// validateStorageConfigurationChange generates an error list by comparing two StorageConfiguration
func validateStorageConfigurationChange(
	structPath *field.Path,
	oldStorage apiv1.StorageConfiguration,
	newStorage apiv1.StorageConfiguration,
) field.ErrorList {
	oldSize := oldStorage.GetSizeOrNil()
	if oldSize == nil {
		// Can't read the old size, so can't tell if the new size is greater
		// or less
		return nil
	}

	newSize := newStorage.GetSizeOrNil()
	if newSize == nil {
		// Can't read the new size, so can't tell if it is increasing
		return nil
	}

	if oldSize.AsDec().Cmp(newSize.AsDec()) < 1 {
		return nil
	}

	return field.ErrorList{
		field.Invalid(
			structPath,
			newSize,
			fmt.Sprintf("can't shrink existing storage from %v to %v", oldSize, newSize)),
	}
}

// Validate the cluster name. This is important to avoid issues
// while generating services, which don't support having dots in
// their name
func (v *ClusterCustomValidator) validateName(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if errs := validationutil.IsDNS1035Label(r.Name); len(errs) > 0 {
		result = append(result, field.Invalid(
			field.NewPath("metadata", "name"),
			r.Name,
			"cluster name must be a valid DNS label"))
	}

	if len(r.Name) > 50 {
		result = append(result, field.Invalid(
			field.NewPath("metadata", "name"),
			r.Name,
			"the maximum length of a cluster name is 50 characters"))
	}

	return result
}

func (v *ClusterCustomValidator) validateTablespaceNames(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	if r.Spec.Tablespaces == nil {
		return nil
	}

	hasTablespace := make(map[string]bool)
	for idx, tbsConfig := range r.Spec.Tablespaces {
		name := tbsConfig.Name
		// NOTE: postgres identifiers are case-insensitive, so we could have
		// different map keys (names) which are identical for PG
		_, found := hasTablespace[strings.ToLower(name)]
		if found {
			result = append(result, field.Invalid(
				field.NewPath("spec", "tablespaces").Index(idx),
				name,
				"duplicate tablespace name"))
			continue
		}
		hasTablespace[strings.ToLower(name)] = true

		if _, err := postgres.IsTablespaceNameValid(name); err != nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "tablespaces").Index(idx),
				name,
				err.Error()))
		}
	}
	return result
}

func (v *ClusterCustomValidator) validateTablespaceBackupSnapshot(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.Backup == nil || r.Spec.Backup.VolumeSnapshot == nil ||
		len(r.Spec.Backup.VolumeSnapshot.TablespaceClassName) == 0 {
		return nil
	}
	backupTbs := r.Spec.Backup.VolumeSnapshot.TablespaceClassName

	var result field.ErrorList
	for name := range backupTbs {
		if tbsConfig := r.GetTablespaceConfiguration(name); tbsConfig == nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "backup", "volumeSnapshot", "tablespaceClassName"),
				name,
				fmt.Sprintf("specified the VolumeSnapshot backup configuration for the tablespace: %s, "+
					"but it can't be found in the '.spec.tablespaces' stanza", name),
			))
		}
	}
	return result
}

// Check if the external clusters list contains two servers with the same name
func (v *ClusterCustomValidator) validateExternalClusters(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	stringSet := stringset.New()

	for idx, externalCluster := range r.Spec.ExternalClusters {
		path := field.NewPath("spec", "externalClusters").Index(idx)
		stringSet.Put(externalCluster.Name)
		result = append(
			result,
			v.validateExternalCluster(&r.Spec.ExternalClusters[idx], path)...)
	}

	if stringSet.Len() != len(r.Spec.ExternalClusters) {
		result = append(result, field.Invalid(
			field.NewPath("spec", "externalClusters"),
			r.Spec.ExternalClusters,
			"the list of external clusters contains duplicate values"))
	}

	return result
}

// validateExternalCluster check the validity of a certain ExternalCluster
func (v *ClusterCustomValidator) validateExternalCluster(
	externalCluster *apiv1.ExternalCluster,
	path *field.Path,
) field.ErrorList {
	var result field.ErrorList

	if externalCluster.ConnectionParameters == nil &&
		externalCluster.BarmanObjectStore == nil &&
		externalCluster.PluginConfiguration == nil {
		result = append(result,
			field.Invalid(
				path,
				externalCluster,
				"one of connectionParameters, plugin and barmanObjectStore is required"))
	}

	return result
}

func (v *ClusterCustomValidator) validateReplicaClusterChange(r, old *apiv1.Cluster) field.ErrorList {
	// If the replication role didn't change then everything
	// is fine
	if r.IsReplica() == old.IsReplica() {
		return nil
	}

	// We disallow changing the replication role when
	// being in a replication cluster switchover
	if r.Status.SwitchReplicaClusterStatus.InProgress {
		return field.ErrorList{
			field.Forbidden(
				field.NewPath("spec", "replica", "enabled"),
				"cannot modify the field while there is an ongoing operation to enable the replica cluster",
			),
		}
	}

	return nil
}

func (v *ClusterCustomValidator) validateUnixPermissionIdentifierChange(r, old *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.PostgresGID != old.Spec.PostgresGID {
		result = append(result, field.Invalid(
			field.NewPath("spec", "postgresGID"),
			r.Spec.PostgresGID,
			"GID is an immutable field in the spec"))
	}

	if r.Spec.PostgresUID != old.Spec.PostgresUID {
		result = append(result, field.Invalid(
			field.NewPath("spec", "postgresUID"),
			r.Spec.PostgresUID,
			"UID is an immutable field in the spec"))
	}

	return result
}

func (v *ClusterCustomValidator) validatePromotionToken(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.ReplicaCluster == nil {
		return result
	}

	token := r.Spec.ReplicaCluster.PromotionToken
	// Nothing to validate if the token is empty, we can immediately return
	if len(token) == 0 {
		return result
	}

	if r.Spec.ReplicaCluster.MinApplyDelay != nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "replicaCluster", "minApplyDelay"),
				token,
				"minApplyDelay cannot be applied with a promotion token"))
		return result
	}

	if r.IsReplica() {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "replicaCluster", "token"),
				token,
				"promotionToken is only allowed for primary clusters"))
		return result
	}

	if !r.IsReplica() {
		tokenContent, err := utils.ParsePgControldataToken(token)
		if err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "replicaCluster", "token"),
					token,
					fmt.Sprintf("Invalid promotionToken format: %s", err.Error())))
		} else if err := tokenContent.IsValid(); err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "replicaCluster", "token"),
					token,
					fmt.Sprintf("Invalid promotionToken content: %s", err.Error())))
		}
	}
	return result
}

// Check if the replica mode is used with an incompatible bootstrap
// method
func (v *ClusterCustomValidator) validateReplicaMode(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	replicaClusterConf := r.Spec.ReplicaCluster
	if replicaClusterConf == nil {
		return result
	}

	// Having enabled set to "true" means that the automatic mode is not active.
	// The "primary" field is used only when the automatic mode is active.
	// This implies that hasEnabled and hasPrimary are mutually exclusive
	hasEnabled := replicaClusterConf.Enabled != nil
	hasPrimary := len(replicaClusterConf.Primary) > 0
	if hasPrimary && hasEnabled {
		result = append(result, field.Invalid(
			field.NewPath("spec", "replicaCluster", "enabled"),
			replicaClusterConf,
			"replica mode enabled is not compatible with the primary field"))
	}

	if r.IsReplica() {
		if r.Spec.Bootstrap == nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap"),
				replicaClusterConf,
				"bootstrap configuration is required for replica mode"))
		} else if r.Spec.Bootstrap.PgBaseBackup == nil && r.Spec.Bootstrap.Recovery == nil &&
			// this is needed because we only want to validate this during cluster creation, currently if we would have
			// to enable this logic only during creation and not cluster changes it would require a meaningful refactor
			len(r.ResourceVersion) == 0 {
			result = append(result, field.Invalid(
				field.NewPath("spec", "replicaCluster"),
				replicaClusterConf,
				"replica mode bootstrap is compatible only with pg_basebackup or recovery"))
		}
	}

	result = append(result, v.validateReplicaClusterExternalClusters(r)...)

	return result
}

func (v *ClusterCustomValidator) validateReplicaClusterExternalClusters(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	replicaClusterConf := r.Spec.ReplicaCluster
	if replicaClusterConf == nil {
		return result
	}

	// Check that the externalCluster references are correct
	_, found := r.ExternalCluster(replicaClusterConf.Source)
	if !found {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "replicaCluster", "primaryServerName"),
				replicaClusterConf.Source,
				fmt.Sprintf("External cluster %v not found", replicaClusterConf.Source)))
	}

	if len(replicaClusterConf.Self) > 0 {
		_, found := r.ExternalCluster(replicaClusterConf.Self)
		if !found {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "replicaCluster", "self"),
					replicaClusterConf.Self,
					fmt.Sprintf("External cluster %v not found", replicaClusterConf.Self)))
		}
	}

	if len(replicaClusterConf.Primary) > 0 {
		_, found := r.ExternalCluster(replicaClusterConf.Primary)
		if !found {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "replicaCluster", "primary"),
					replicaClusterConf.Primary,
					fmt.Sprintf("External cluster %v not found", replicaClusterConf.Primary)))
		}
	}
	return result
}

// validateTolerations check and validate the tolerations field
// This code is almost a verbatim copy of
// https://github.com/kubernetes/kubernetes/blob/4d38d21/pkg/apis/core/validation/validation.go#L3147
func (v *ClusterCustomValidator) validateTolerations(r *apiv1.Cluster) field.ErrorList {
	path := field.NewPath("spec", "affinity", "toleration")
	allErrors := field.ErrorList{}
	for i, toleration := range r.Spec.Affinity.Tolerations {
		idxPath := path.Index(i)
		// validate the toleration key
		if len(toleration.Key) > 0 {
			allErrors = append(allErrors, validation.ValidateLabelName(toleration.Key, idxPath.Child("key"))...)
		}

		// empty toleration key with Exists operator and empty value means match all taints
		if len(toleration.Key) == 0 && toleration.Operator != corev1.TolerationOpExists {
			allErrors = append(allErrors,
				field.Invalid(idxPath.Child("operator"),
					toleration.Operator,
					"operator must be Exists when `key` is empty, which means \"match all values and all keys\""))
		}

		if toleration.TolerationSeconds != nil && toleration.Effect != corev1.TaintEffectNoExecute {
			allErrors = append(allErrors,
				field.Invalid(idxPath.Child("effect"),
					toleration.Effect,
					"effect must be 'NoExecute' when `tolerationSeconds` is set"))
		}

		// validate toleration operator and value
		switch toleration.Operator {
		// empty operator means Equal
		case corev1.TolerationOpEqual, "":
			if errs := validationutil.IsValidLabelValue(toleration.Value); len(errs) != 0 {
				allErrors = append(allErrors,
					field.Invalid(idxPath.Child("operator"),
						toleration.Value, strings.Join(errs, ";")))
			}
		case corev1.TolerationOpExists:
			if len(toleration.Value) > 0 {
				allErrors = append(allErrors,
					field.Invalid(idxPath.Child("operator"),
						toleration, "value must be empty when `operator` is 'Exists'"))
			}
		default:
			validValues := []string{string(corev1.TolerationOpEqual), string(corev1.TolerationOpExists)}
			allErrors = append(allErrors,
				field.NotSupported(idxPath.Child("operator"),
					toleration.Operator, validValues))
		}

		// validate toleration effect, empty toleration effect means match all taint effects
		if len(toleration.Effect) > 0 {
			allErrors = append(allErrors, validateTaintEffect(&toleration.Effect, true, idxPath.Child("effect"))...)
		}
	}

	return allErrors
}

// validateTaintEffect is used from validateToleration and is a verbatim copy of the code
// at https://github.com/kubernetes/kubernetes/blob/4d38d21/pkg/apis/core/validation/validation.go#L3087
func validateTaintEffect(effect *corev1.TaintEffect, allowEmpty bool, fldPath *field.Path) field.ErrorList {
	if !allowEmpty && len(*effect) == 0 {
		return field.ErrorList{field.Required(fldPath, "")}
	}

	allErrors := field.ErrorList{}
	switch *effect {
	// TODO: Replace next line with subsequent commented-out line when implement TaintEffectNoScheduleNoAdmit.
	case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		// case core.TaintEffectNoSchedule, core.TaintEffectPreferNoSchedule, core.TaintEffectNoScheduleNoAdmit,
		//     core.TaintEffectNoExecute:
	default:
		validValues := []string{
			string(corev1.TaintEffectNoSchedule),
			string(corev1.TaintEffectPreferNoSchedule),
			string(corev1.TaintEffectNoExecute),
			// TODO: Uncomment this block when implement TaintEffectNoScheduleNoAdmit.
			// string(core.TaintEffectNoScheduleNoAdmit),
		}
		allErrors = append(allErrors, field.NotSupported(fldPath, *effect, validValues))
	}
	return allErrors
}

// validateAntiAffinity checks and validates the anti-affinity fields.
func (v *ClusterCustomValidator) validateAntiAffinity(r *apiv1.Cluster) field.ErrorList {
	path := field.NewPath("spec", "affinity", "podAntiAffinityType")
	allErrors := field.ErrorList{}

	if r.Spec.Affinity.PodAntiAffinityType != apiv1.PodAntiAffinityTypePreferred &&
		r.Spec.Affinity.PodAntiAffinityType != apiv1.PodAntiAffinityTypeRequired &&
		r.Spec.Affinity.PodAntiAffinityType != "" {
		allErrors = append(allErrors, field.Invalid(
			path,
			r.Spec.Affinity.PodAntiAffinityType,
			fmt.Sprintf("pod anti-affinity type must be '%s' (default if empty) or '%s'",
				apiv1.PodAntiAffinityTypePreferred, apiv1.PodAntiAffinityTypeRequired),
		))
	}
	return allErrors
}

// validateBackupConfiguration validates the backup configuration
func (v *ClusterCustomValidator) validateBackupConfiguration(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.Backup == nil {
		return nil
	}
	return barmanWebhooks.ValidateBackupConfiguration(
		r.Spec.Backup.BarmanObjectStore,
		field.NewPath("spec", "backup", "barmanObjectStore"),
	)
}

// validateRetentionPolicy validates the retention policy configuration
func (v *ClusterCustomValidator) validateRetentionPolicy(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.Backup == nil {
		return nil
	}
	return barmanWebhooks.ValidateRetentionPolicy(
		r.Spec.Backup.RetentionPolicy,
		field.NewPath("spec", "backup", "retentionPolicy"),
	)
}

func (v *ClusterCustomValidator) validateReplicationSlots(r *apiv1.Cluster) field.ErrorList {
	if r.Spec.ReplicationSlots == nil {
		r.Spec.ReplicationSlots = &apiv1.ReplicationSlotsConfiguration{
			HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
				Enabled: ptr.To(true),
			},
			SynchronizeReplicas: &apiv1.SynchronizeReplicasConfiguration{
				Enabled: ptr.To(true),
			},
		}
	}
	replicationSlots := r.Spec.ReplicationSlots

	if !replicationSlots.GetEnabled() {
		return nil
	}

	if err := r.Spec.ReplicationSlots.SynchronizeReplicas.ValidateRegex(); err != nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "replicationSlots", "synchronizeReplicas", "excludePatterns"),
				err,
				"Cannot configure synchronizeReplicas. Invalid regexes were found"),
		}
	}

	return nil
}

func (v *ClusterCustomValidator) validateSynchronizeLogicalDecoding(r *apiv1.Cluster) field.ErrorList {
	replicationSlots := r.Spec.ReplicationSlots
	if replicationSlots.HighAvailability == nil || !replicationSlots.HighAvailability.SynchronizeLogicalDecoding {
		return nil
	}

	if postgres.IsManagedExtensionUsed("pg_failover_slots", r.Spec.PostgresConfiguration.Parameters) {
		return nil
	}

	pgMajor, err := r.GetPostgresqlMajorVersion()
	if err != nil {
		return nil
	}

	if pgMajor < 17 {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "replicationSlots", "highAvailability", "synchronizeLogicalDecoding"),
				replicationSlots.HighAvailability.SynchronizeLogicalDecoding,
				"pg_failover_slots extension must be enabled to use synchronizeLogicalDecoding with Postgres versions < 17",
			),
		}
	}

	result := field.ErrorList{}

	hotStandbyFeedback, _ := postgres.ParsePostgresConfigBoolean(
		r.Spec.PostgresConfiguration.Parameters[postgres.ParameterHotStandbyFeedback])
	if !hotStandbyFeedback {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", postgres.ParameterHotStandbyFeedback),
				hotStandbyFeedback,
				fmt.Sprintf("`%s` must be enabled to enable "+
					"`spec.replicationSlots.highAvailability.synchronizeLogicalDecoding`",
					postgres.ParameterHotStandbyFeedback)))
	}

	const syncReplicationSlotsKey = "sync_replication_slots"
	syncReplicationSlots, _ := postgres.ParsePostgresConfigBoolean(
		r.Spec.PostgresConfiguration.Parameters[syncReplicationSlotsKey])
	if !syncReplicationSlots {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", syncReplicationSlotsKey),
				syncReplicationSlots,
				fmt.Sprintf("either `%s` setting or pg_failover_slots extension must be enabled to enable "+
					"`spec.replicationSlots.highAvailability.synchronizeLogicalDecoding`", syncReplicationSlotsKey)))
	}

	return result
}

func (v *ClusterCustomValidator) validateReplicationSlotsChange(r, old *apiv1.Cluster) field.ErrorList {
	newReplicationSlots := r.Spec.ReplicationSlots
	oldReplicationSlots := old.Spec.ReplicationSlots

	var errs field.ErrorList

	if oldReplicationSlots == nil {
		return nil
	}

	// Validate HighAvailability changes
	if oldReplicationSlots.HighAvailability.GetEnabled() {
		// When disabling, we should check that the prefix is not removed and doesn't change
		// to properly execute the cleanup logic
		if newReplicationSlots == nil || newReplicationSlots.HighAvailability == nil {
			path := field.NewPath("spec", "replicationSlots")
			if newReplicationSlots != nil {
				path = path.Child("highAvailability")
			}
			errs = append(errs,
				field.Invalid(
					path,
					nil,
					fmt.Sprintf("Cannot remove %v section while highAvailability is enabled", path)),
			)
		} else if oldReplicationSlots.HighAvailability.SlotPrefix != newReplicationSlots.HighAvailability.SlotPrefix {
			errs = append(errs,
				field.Invalid(
					field.NewPath("spec", "replicationSlots", "highAvailability", "slotPrefix"),
					newReplicationSlots.HighAvailability.SlotPrefix,
					"Cannot change replication slot prefix while highAvailability is enabled"),
			)
		}
	}

	// Validate SynchronizeReplicas changes
	// When synchronizeReplicas is enabled, we need to ensure users disable it before removing the configuration
	// to allow the cleanup logic to properly remove user-defined replication slots from replicas
	if oldReplicationSlots.SynchronizeReplicas.GetEnabled() {
		if newReplicationSlots == nil || newReplicationSlots.SynchronizeReplicas == nil {
			path := field.NewPath("spec", "replicationSlots")
			if newReplicationSlots != nil {
				path = path.Child("synchronizeReplicas")
			}
			errs = append(errs,
				field.Invalid(
					path,
					nil,
					fmt.Sprintf("Cannot remove %v section while synchronizeReplicas is enabled. "+
						"Disable synchronizeReplicas first to allow cleanup of user-defined replication slots on replicas", path)),
			)
		}
	}

	return errs
}

func (v *ClusterCustomValidator) validateWALLevelChange(r, old *apiv1.Cluster) field.ErrorList {
	var errs field.ErrorList

	newWALLevel := r.Spec.PostgresConfiguration.Parameters[postgres.ParameterWalLevel]
	oldWALLevel := old.Spec.PostgresConfiguration.Parameters[postgres.ParameterWalLevel]

	if newWALLevel == "minimal" && len(oldWALLevel) > 0 && oldWALLevel != newWALLevel {
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "postgresql", "parameters", "wal_level"),
			"minimal",
			fmt.Sprintf("Change of `wal_level` to `minimal` not allowed on an existing cluster (from %s)",
				oldWALLevel)))
	}

	return errs
}

func (v *ClusterCustomValidator) validateManagedServices(r *apiv1.Cluster) field.ErrorList {
	reservedNames := []string{
		r.GetServiceReadWriteName(),
		r.GetServiceReadOnlyName(),
		r.GetServiceReadName(),
		r.GetServiceAnyName(),
	}
	containsDuplicateNames := func(names []string) bool {
		seen := make(map[string]bool)
		for _, str := range names {
			if seen[str] {
				return true
			}
			seen[str] = true
		}
		return false
	}

	if r.Spec.Managed == nil || r.Spec.Managed.Services == nil {
		return nil
	}

	managedServices := r.Spec.Managed.Services
	basePath := field.NewPath("spec", "managed", "services")
	var errs field.ErrorList

	if slices.Contains(managedServices.DisabledDefaultServices, apiv1.ServiceSelectorTypeRW) {
		errs = append(errs, field.Invalid(
			basePath.Child("disabledDefaultServices"),
			apiv1.ServiceSelectorTypeRW,
			"service of type RW cannot be disabled.",
		))
	}

	names := make([]string, len(managedServices.Additional))
	for idx := range managedServices.Additional {
		additionalService := &managedServices.Additional[idx]
		name := additionalService.ServiceTemplate.ObjectMeta.Name
		names[idx] = name
		path := basePath.Child(fmt.Sprintf("additional[%d]", idx))

		if slices.Contains(reservedNames, name) {
			errs = append(errs,
				field.Invalid(
					path,
					name,
					fmt.Sprintf("the service name: '%s' is reserved for operator use", name),
				))
		}

		if fieldErr := validateServiceTemplate(
			path,
			true,
			additionalService.ServiceTemplate,
		); len(fieldErr) > 0 {
			errs = append(errs, fieldErr...)
		}
	}

	if containsDuplicateNames(names) {
		errs = append(errs, field.Invalid(
			basePath.Child("additional"),
			names,
			"contains services with the same .metadata.name",
		))
	}

	return errs
}

func validateServiceTemplate(
	path *field.Path,
	nameRequired bool,
	template apiv1.ServiceTemplateSpec,
) field.ErrorList {
	var errs field.ErrorList

	if len(template.Spec.Selector) > 0 {
		errs = append(errs, field.Invalid(path, template.Spec.Selector, "selector field is managed by the operator"))
	}

	name := template.ObjectMeta.Name
	if name == "" && nameRequired {
		errs = append(errs, field.Invalid(path, name, "name is required"))
	}
	if name != "" && !nameRequired {
		errs = append(errs, field.Invalid(path, name, "name is not allowed"))
	}

	return errs
}

// validateManagedRoles validate the environment variables settings proposed by the user
func (v *ClusterCustomValidator) validateManagedRoles(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList

	if r.Spec.Managed == nil {
		return nil
	}

	managedRoles := make(map[string]interface{})
	for _, role := range r.Spec.Managed.Roles {
		_, found := managedRoles[role.Name]
		if found {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "managed", "roles"),
					role.Name,
					"Role name is duplicate of another"))
		}
		managedRoles[role.Name] = nil
		if role.ConnectionLimit != -1 && role.ConnectionLimit < 0 {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "managed", "roles"),
					role.ConnectionLimit,
					"Connection limit should be positive, unless defaulting to -1"))
		}
		if postgres.IsRoleReserved(role.Name) {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "managed", "roles"),
					role.Name,
					"This role is reserved for operator use"))
		}
		if role.DisablePassword && role.PasswordSecret != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "managed", "roles"),
					role.Name,
					"This role both sets and disables a password"))
		}
	}

	return result
}

// validateManagedExtensions validate the managed extensions parameters set by the user
func (v *ClusterCustomValidator) validateManagedExtensions(r *apiv1.Cluster) field.ErrorList {
	allErrors := field.ErrorList{}

	allErrors = append(allErrors, v.validatePgFailoverSlots(r)...)
	return allErrors
}

func (v *ClusterCustomValidator) validatePgFailoverSlots(r *apiv1.Cluster) field.ErrorList {
	var result field.ErrorList
	var pgFailoverSlots postgres.ManagedExtension

	if !postgres.IsManagedExtensionUsed("pg_failover_slots", r.Spec.PostgresConfiguration.Parameters) {
		return nil
	}

	hotStandbyFeedback, _ := postgres.ParsePostgresConfigBoolean(
		r.Spec.PostgresConfiguration.Parameters[postgres.ParameterHotStandbyFeedback])
	if !hotStandbyFeedback {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", postgres.ParameterHotStandbyFeedback),
				hotStandbyFeedback,
				fmt.Sprintf("`%s` must be enabled to use %s extension",
					postgres.ParameterHotStandbyFeedback, pgFailoverSlots.Name)))
	}

	if r.Spec.ReplicationSlots == nil {
		return append(result,
			field.Invalid(
				field.NewPath("spec", "replicationSlots"),
				nil,
				"replicationSlots must be enabled"),
		)
	}

	if r.Spec.ReplicationSlots.HighAvailability == nil ||
		!r.Spec.ReplicationSlots.HighAvailability.GetEnabled() {
		return append(result,
			field.Invalid(
				field.NewPath("spec", "replicationSlots", "highAvailability"),
				"nil or false",
				"High Availability replication slots must be enabled"),
		)
	}

	return result
}

func (v *ClusterCustomValidator) getAdmissionWarnings(r *apiv1.Cluster) admission.Warnings {
	list := getMaintenanceWindowsAdmissionWarnings(r)
	list = append(list, getInTreeBarmanWarnings(r)...)
	list = append(list, getRetentionPolicyWarnings(r)...)
	list = append(list, getStorageWarnings(r)...)
	list = append(list, getSharedBuffersWarnings(r)...)
	list = append(list, getMonitoringFieldsWarnings(r)...)
	return append(list, getDeprecatedMonitoringFieldsWarnings(r)...)
}

func getMonitoringFieldsWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	if r.GetMetricsQueriesTTL().Duration == 0 {
		result = append(result,
			"spec.monitoring.metricsQueriesTTL is explicitly set to 0; this disables automatic TTL behavior "+
				"and can cause heavy load on the PostgreSQL server.")
	}

	return result
}

func getStorageWarnings(r *apiv1.Cluster) admission.Warnings {
	generateWarningsFunc := func(path field.Path, configuration *apiv1.StorageConfiguration) admission.Warnings {
		if configuration == nil {
			return nil
		}

		if configuration.PersistentVolumeClaimTemplate == nil {
			return nil
		}

		pvcTemplatePath := path.Child("pvcTemplate")

		var result admission.Warnings
		if configuration.StorageClass != nil && configuration.PersistentVolumeClaimTemplate.StorageClassName != nil {
			storageClass := path.Child("storageClass").String()
			result = append(
				result,
				fmt.Sprintf("%s and %s are both specified, %s value will be used.",
					storageClass,
					pvcTemplatePath.Child("storageClassName"),
					storageClass,
				),
			)
		}
		requestsSpecified := !configuration.PersistentVolumeClaimTemplate.Resources.Requests.Storage().IsZero()
		if configuration.Size != "" && requestsSpecified {
			size := path.Child("size").String()
			result = append(
				result,
				fmt.Sprintf(
					"%s and %s are both specified, %s value will be used.",
					size,
					pvcTemplatePath.Child("resources", "requests", "storage").String(),
					size,
				),
			)
		}

		return result
	}

	var result admission.Warnings

	storagePath := *field.NewPath("spec", "storage")
	result = append(result, generateWarningsFunc(storagePath, &r.Spec.StorageConfiguration)...)

	walStoragePath := *field.NewPath("spec", "walStorage")
	return append(result, generateWarningsFunc(walStoragePath, r.Spec.WalStorage)...)
}

func getInTreeBarmanWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	var paths []string

	if r.Spec.Backup != nil && r.Spec.Backup.BarmanObjectStore != nil {
		paths = append(paths, field.NewPath("spec", "backup", "barmanObjectStore").String())
	}

	for idx, externalCluster := range r.Spec.ExternalClusters {
		if externalCluster.BarmanObjectStore != nil {
			paths = append(paths, field.NewPath("spec", "externalClusters", fmt.Sprintf("%d", idx),
				"barmanObjectStore").String())
		}
	}

	if len(paths) > 0 {
		pathsStr := strings.Join(paths, ", ")
		result = append(
			result,
			fmt.Sprintf("Native support for Barman Cloud backups and recovery is deprecated and will be "+
				"completely removed in CloudNativePG 1.29.0. Found usage in: %s. "+
				"Please migrate existing clusters to the new Barman Cloud Plugin to ensure a smooth transition.",
				pathsStr),
		)
	}
	return result
}

func getRetentionPolicyWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	if r.Spec.Backup != nil && r.Spec.Backup.RetentionPolicy != "" && r.Spec.Backup.BarmanObjectStore == nil {
		result = append(
			result,
			"Retention policies specified in .spec.backup.retentionPolicy are only used by the "+
				"in-tree barman-cloud support, which is not being used in this cluster. "+
				"Please use a backup plugin and migrate this configuration to the plugin configuration",
		)
	}

	return result
}

func getSharedBuffersWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	if v := r.Spec.PostgresConfiguration.Parameters["shared_buffers"]; v != "" {
		if _, err := strconv.Atoi(v); err == nil {
			result = append(
				result,
				fmt.Sprintf("`shared_buffers` value '%s' is missing a unit (e.g., MB, GB). "+
					"While this is currently allowed, future releases will require an explicit unit. "+
					"Please update your configuration to specify a valid unit, such as '%sMB'.", v, v),
			)
		}
	}
	return result
}

func getDeprecatedMonitoringFieldsWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	if r.Spec.Monitoring != nil {
		//nolint:staticcheck // Checking deprecated fields to warn users
		if r.Spec.Monitoring.EnablePodMonitor {
			result = append(result,
				"spec.monitoring.enablePodMonitor is deprecated and will be removed in a future release. "+
					"Please migrate to manually managing your PodMonitor resources. "+
					"Set this field to false and create a PodMonitor resource for your cluster as described in the documentation.")
		}
		//nolint:staticcheck // Checking deprecated fields to warn users
		if len(r.Spec.Monitoring.PodMonitorMetricRelabelConfigs) > 0 {
			result = append(result,
				"spec.monitoring.podMonitorMetricRelabelings is deprecated and will be removed in a future release. "+
					"Please migrate to manually managing your PodMonitor resources with custom relabeling configurations.")
		}
		//nolint:staticcheck // Checking deprecated fields to warn users
		if len(r.Spec.Monitoring.PodMonitorRelabelConfigs) > 0 {
			result = append(result,
				"spec.monitoring.podMonitorRelabelings is deprecated and will be removed in a future release. "+
					"Please migrate to manually managing your PodMonitor resources with custom relabeling configurations.")
		}
	}

	return result
}

func getMaintenanceWindowsAdmissionWarnings(r *apiv1.Cluster) admission.Warnings {
	var result admission.Warnings

	if r.Spec.NodeMaintenanceWindow != nil {
		result = append(
			result,
			"Consider using `.spec.enablePDB` instead of the node maintenance window feature")
	}
	return result
}

// validate whether the hibernation configuration is valid
func (v *ClusterCustomValidator) validateHibernationAnnotation(r *apiv1.Cluster) field.ErrorList {
	value, ok := r.Annotations[utils.HibernationAnnotationName]
	isKnownValue := value == string(utils.HibernationAnnotationValueOn) ||
		value == string(utils.HibernationAnnotationValueOff)
	if !ok || isKnownValue {
		return nil
	}

	return field.ErrorList{
		field.Invalid(
			field.NewPath("metadata", "annotations", utils.HibernationAnnotationName),
			value,
			fmt.Sprintf("Annotation value for hibernation should be %q or %q",
				utils.HibernationAnnotationValueOn,
				utils.HibernationAnnotationValueOff,
			),
		),
	}
}

func (v *ClusterCustomValidator) validatePodPatchAnnotation(r *apiv1.Cluster) field.ErrorList {
	jsonPatch, ok := r.Annotations[utils.PodPatchAnnotationName]
	if !ok {
		return nil
	}

	if _, err := jsonpatch.DecodePatch([]byte(jsonPatch)); err != nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("metadata", "annotations", utils.PodPatchAnnotationName),
				jsonPatch,
				fmt.Sprintf("error decoding JSON patch: %s", err.Error()),
			),
		}
	}

	if _, err := specs.NewInstance(
		context.Background(),
		*r,
		1,
		true,
	); err != nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("metadata", "annotations", utils.PodPatchAnnotationName),
				jsonPatch,
				fmt.Sprintf("jsonpatch doesn't apply cleanly to the pod: %s", err.Error()),
			),
		}
	}

	return nil
}

func (v *ClusterCustomValidator) validatePluginConfiguration(r *apiv1.Cluster) field.ErrorList {
	if len(r.Spec.Plugins) == 0 {
		return nil
	}
	isBarmanObjectStoreConfigured := r.Spec.Backup != nil && r.Spec.Backup.BarmanObjectStore != nil
	var walArchiverEnabled []string

	for _, plugin := range r.Spec.Plugins {
		if !plugin.IsEnabled() {
			continue
		}
		if plugin.IsWALArchiver != nil && *plugin.IsWALArchiver {
			walArchiverEnabled = append(walArchiverEnabled, plugin.Name)
		}
	}

	var errorList field.ErrorList
	if isBarmanObjectStoreConfigured {
		if len(walArchiverEnabled) > 0 {
			errorList = append(errorList, field.Invalid(
				field.NewPath("spec", "plugins"),
				walArchiverEnabled,
				"Cannot enable a WAL archiver plugin when barmanObjectStore is configured"))
		}
	}

	if len(walArchiverEnabled) > 1 {
		errorList = append(errorList, field.Invalid(
			field.NewPath("spec", "plugins"),
			walArchiverEnabled,
			"Cannot enable more than one WAL archiver plugin"))
	}

	return errorList
}

func (v *ClusterCustomValidator) validateLivenessPingerProbe(r *apiv1.Cluster) field.ErrorList {
	value, ok := r.Annotations[utils.LivenessPingerAnnotationName]
	if !ok {
		return nil
	}

	_, err := apiv1.NewLivenessPingerConfigFromAnnotations(r.Annotations)
	if err != nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("metadata", "annotations", utils.LivenessPingerAnnotationName),
				value,
				fmt.Sprintf("error decoding liveness pinger config: %s", err.Error()),
			),
		}
	}

	return nil
}

func (v *ClusterCustomValidator) validateExtensions(r *apiv1.Cluster) field.ErrorList {
	ensureNotEmptyOrDuplicate := func(path *field.Path, list *stringset.Data, value string) *field.Error {
		if value == "" {
			return field.Invalid(
				path,
				value,
				"value cannot be empty",
			)
		}

		if list.Has(value) {
			return field.Duplicate(
				path,
				value,
			)
		}
		return nil
	}

	if len(r.Spec.PostgresConfiguration.Extensions) == 0 {
		return nil
	}

	var result field.ErrorList

	extensionNames := stringset.New()
	// Track sanitized volume names (e.g., pg_ivm and pg-ivm both become pg-ivm)
	sanitizedVolumeNames := stringset.New()

	for i, v := range r.Spec.PostgresConfiguration.Extensions {
		basePath := field.NewPath("spec", "postgresql", "extensions").Index(i)
		if nameErr := ensureNotEmptyOrDuplicate(basePath.Child("name"), extensionNames, v.Name); nameErr != nil {
			result = append(result, nameErr)
			// Skip sanitization check for duplicate names to avoid redundant error reporting
			continue
		}
		extensionNames.Put(v.Name)

		sanitizedName := strings.ReplaceAll(v.Name, "_", "-")
		if sanitizedVolumeNames.Has(sanitizedName) {
			result = append(result, field.Invalid(
				basePath.Child("name"),
				v.Name,
				fmt.Sprintf("extension name results in duplicate volume name %q after sanitization "+
					"(underscores are converted to hyphens)", sanitizedName),
			))
		}
		sanitizedVolumeNames.Put(sanitizedName)

		controlPaths := stringset.New()
		for j, path := range v.ExtensionControlPath {
			if validateErr := ensureNotEmptyOrDuplicate(
				basePath.Child("extension_control_path").Index(j),
				controlPaths,
				path,
			); validateErr != nil {
				result = append(result, validateErr)
			}

			controlPaths.Put(path)
		}

		libraryPaths := stringset.New()
		for j, path := range v.DynamicLibraryPath {
			if validateErr := ensureNotEmptyOrDuplicate(
				basePath.Child("dynamic_library_path").Index(j),
				libraryPaths,
				path,
			); validateErr != nil {
				result = append(result, validateErr)
			}

			libraryPaths.Put(path)
		}
	}

	return result
}
