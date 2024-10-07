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

package v1

import (
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
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutil "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// DefaultMonitoringKey is the key that should be used in the default metrics configmap to store the queries
	DefaultMonitoringKey = "queries"
	// DefaultMonitoringConfigMapName is the name of the target configmap with the default monitoring queries,
	// if configured
	DefaultMonitoringConfigMapName = "cnpg-default-monitoring"
	// DefaultMonitoringSecretName is the name of the target secret with the default monitoring queries,
	// if configured
	DefaultMonitoringSecretName = DefaultMonitoringConfigMapName
	// DefaultApplicationDatabaseName is the name of application database if not specified
	DefaultApplicationDatabaseName = "app"
	// DefaultApplicationUserName is the name of application database owner if not specified
	DefaultApplicationUserName = DefaultApplicationDatabaseName
)

const sharedBuffersParameter = "shared_buffers"

// clusterLog is for logging in this package.
var clusterLog = log.WithName("cluster-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,verbs=create;update,versions=v1,name=mcluster.cnpg.io,sideEffects=None

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	clusterLog.Info("default", "name", r.Name, "namespace", r.Namespace)

	r.setDefaults(true)
}

// SetDefaults apply the defaults to undefined values in a Cluster
func (r *Cluster) SetDefaults() {
	r.setDefaults(false)
}

func (r *Cluster) setDefaults(preserveUserSettings bool) {
	// Defaulting the image name if not specified
	if r.Spec.ImageName == "" && r.Spec.ImageCatalogRef == nil {
		r.Spec.ImageName = configuration.Current.PostgresImageName
	}

	// Defaulting the bootstrap method if not specified
	if r.Spec.Bootstrap == nil {
		r.Spec.Bootstrap = &BootstrapConfiguration{}
	}

	// Defaulting initDB if no other boostrap method was passed
	switch {
	case r.Spec.Bootstrap.Recovery != nil:
		r.defaultRecovery()
	case r.Spec.Bootstrap.PgBaseBackup != nil:
		r.defaultPgBaseBackup()
	default:
		r.defaultInitDB()
	}

	// Defaulting the pod anti-affinity type if podAntiAffinity
	if (r.Spec.Affinity.EnablePodAntiAffinity == nil || *r.Spec.Affinity.EnablePodAntiAffinity) &&
		r.Spec.Affinity.PodAntiAffinityType == "" {
		r.Spec.Affinity.PodAntiAffinityType = PodAntiAffinityTypePreferred
	}

	if r.Spec.Backup != nil && r.Spec.Backup.Target == "" {
		r.Spec.Backup.Target = DefaultBackupTarget
	}

	psqlVersion, err := r.GetPostgresqlVersion()
	if err == nil {
		// The validation error will be already raised by the
		// validateImageName function
		info := postgres.ConfigurationInfo{
			Settings:                      postgres.CnpgConfigurationSettings,
			Version:                       psqlVersion,
			UserSettings:                  r.Spec.PostgresConfiguration.Parameters,
			IsReplicaCluster:              r.IsReplica(),
			PreserveFixedSettingsFromUser: preserveUserSettings,
			IsWalArchivingDisabled:        utils.IsWalArchivingDisabled(&r.ObjectMeta),
			IsAlterSystemEnabled:          r.Spec.PostgresConfiguration.EnableAlterSystem,
		}
		sanitizedParameters := postgres.CreatePostgresqlConfiguration(info).GetConfigurationParameters()
		r.Spec.PostgresConfiguration.Parameters = sanitizedParameters
	}

	if r.Spec.LogLevel == "" {
		r.Spec.LogLevel = log.InfoLevelString
	}

	// we inject the defaultMonitoringQueries if the MonitoringQueriesConfigmap parameter is not empty
	// and defaultQueries not disabled on cluster crd
	if !r.Spec.Monitoring.AreDefaultQueriesDisabled() {
		r.defaultMonitoringQueries(configuration.Current)
	}

	// If the ReplicationSlots or HighAvailability stanzas are nil, we create them and enable slots
	if r.Spec.ReplicationSlots == nil {
		r.Spec.ReplicationSlots = &ReplicationSlotsConfiguration{}
	}
	if r.Spec.ReplicationSlots.HighAvailability == nil {
		r.Spec.ReplicationSlots.HighAvailability = &ReplicationSlotsHAConfiguration{
			Enabled:    ptr.To(true),
			SlotPrefix: "_cnpg_",
		}
	}
	if r.Spec.ReplicationSlots.SynchronizeReplicas == nil {
		r.Spec.ReplicationSlots.SynchronizeReplicas = &SynchronizeReplicasConfiguration{
			Enabled: ptr.To(true),
		}
	}

	if len(r.Spec.Tablespaces) > 0 {
		r.defaultTablespaces()
	}

	r.setDefaultPlugins(configuration.Current)
}

func (r *Cluster) setDefaultPlugins(config *configuration.Data) {
	// Add the list of pre-defined plugins
	foundPlugins := stringset.New()
	for _, plugin := range r.Spec.Plugins {
		foundPlugins.Put(plugin.Name)
	}

	for _, pluginName := range config.GetIncludePlugins() {
		if !foundPlugins.Has(pluginName) {
			r.Spec.Plugins = append(r.Spec.Plugins, PluginConfiguration{
				Name:    pluginName,
				Enabled: ptr.To(true),
			})
		}
	}
}

// defaultTablespaces adds the tablespace owner where the
// user didn't specify it
func (r *Cluster) defaultTablespaces() {
	defaultOwner := r.GetApplicationDatabaseOwner()
	if len(defaultOwner) == 0 {
		defaultOwner = "postgres"
	}

	for name, tablespaceConfiguration := range r.Spec.Tablespaces {
		if len(tablespaceConfiguration.Owner.Name) == 0 {
			tablespaceConfiguration.Owner.Name = defaultOwner
		}
		r.Spec.Tablespaces[name] = tablespaceConfiguration
	}
}

// defaultMonitoringQueries adds the default monitoring queries configMap
// if not already present in CustomQueriesConfigMap
func (r *Cluster) defaultMonitoringQueries(config *configuration.Data) {
	if r.Spec.Monitoring == nil {
		r.Spec.Monitoring = &MonitoringConfiguration{}
	}

	if config.MonitoringQueriesConfigmap != "" {
		var defaultConfigMapQueriesAlreadyPresent bool
		// We check if the default queries are already inserted in the monitoring configuration
		for _, monitoringConfigMap := range r.Spec.Monitoring.CustomQueriesConfigMap {
			if monitoringConfigMap.Name == DefaultMonitoringConfigMapName {
				defaultConfigMapQueriesAlreadyPresent = true
				break
			}
		}

		// If the default queries are already present there is no need to re-add them.
		// Please note that in this case that the default configMap could overwrite user existing queries
		// depending on the order. This is an accepted behavior because the user willingly defined the order of his array
		if !defaultConfigMapQueriesAlreadyPresent {
			r.Spec.Monitoring.CustomQueriesConfigMap = append([]ConfigMapKeySelector{
				{
					LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
					Key:                  DefaultMonitoringKey,
				},
			}, r.Spec.Monitoring.CustomQueriesConfigMap...)
		}
	}

	if config.MonitoringQueriesSecret != "" {
		var defaultSecretQueriesAlreadyPresent bool
		// we check if the default queries are already inserted in the monitoring configuration
		for _, monitoringSecret := range r.Spec.Monitoring.CustomQueriesSecret {
			if monitoringSecret.Name == DefaultMonitoringSecretName {
				defaultSecretQueriesAlreadyPresent = true
				break
			}
		}

		if !defaultSecretQueriesAlreadyPresent {
			r.Spec.Monitoring.CustomQueriesSecret = append([]SecretKeySelector{
				{
					LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringSecretName},
					Key:                  DefaultMonitoringKey,
				},
			}, r.Spec.Monitoring.CustomQueriesSecret...)
		}
	}
}

// defaultInitDB enriches the initDB with defaults if not all the required arguments were passed
func (r *Cluster) defaultInitDB() {
	if r.Spec.Bootstrap.InitDB == nil {
		r.Spec.Bootstrap.InitDB = &BootstrapInitDB{
			Database: DefaultApplicationDatabaseName,
			Owner:    DefaultApplicationUserName,
		}
	}

	if r.Spec.Bootstrap.InitDB.Database == "" {
		r.Spec.Bootstrap.InitDB.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.InitDB.Owner == "" {
		r.Spec.Bootstrap.InitDB.Owner = r.Spec.Bootstrap.InitDB.Database
	}
	if r.Spec.Bootstrap.InitDB.Encoding == "" {
		r.Spec.Bootstrap.InitDB.Encoding = "UTF8"
	}
	if r.Spec.Bootstrap.InitDB.LocaleCollate == "" {
		r.Spec.Bootstrap.InitDB.LocaleCollate = "C"
	}
	if r.Spec.Bootstrap.InitDB.LocaleCType == "" {
		r.Spec.Bootstrap.InitDB.LocaleCType = "C"
	}
}

// defaultRecovery enriches the recovery with defaults if not all the required arguments were passed
func (r *Cluster) defaultRecovery() {
	if r.Spec.Bootstrap.Recovery.Database == "" {
		r.Spec.Bootstrap.Recovery.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.Recovery.Owner == "" {
		r.Spec.Bootstrap.Recovery.Owner = r.Spec.Bootstrap.Recovery.Database
	}
}

// defaultPgBaseBackup enriches the pg_basebackup with defaults if not all the required arguments were passed
func (r *Cluster) defaultPgBaseBackup() {
	if r.Spec.Bootstrap.PgBaseBackup.Database == "" {
		r.Spec.Bootstrap.PgBaseBackup.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.PgBaseBackup.Owner == "" {
		r.Spec.Bootstrap.PgBaseBackup.Owner = r.Spec.Bootstrap.PgBaseBackup.Database
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,versions=v1,name=vcluster.cnpg.io,sideEffects=None

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() (admission.Warnings, error) {
	clusterLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)
	allErrs := r.Validate()
	allWarnings := r.getAdmissionWarnings()

	if len(allErrs) == 0 {
		return allWarnings, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// Validate groups the validation logic for clusters returning a list of all encountered errors
func (r *Cluster) Validate() (allErrs field.ErrorList) {
	type validationFunc func() field.ErrorList
	validations := []validationFunc{
		r.validateInitDB,
		r.validateRecoveryApplicationDatabase,
		r.validatePgBaseBackupApplicationDatabase,
		r.validateImport,
		r.validateSuperuserSecret,
		r.validateCerts,
		r.validateBootstrapMethod,
		r.validateImageName,
		r.validateImagePullPolicy,
		r.validateRecoveryTarget,
		r.validatePrimaryUpdateStrategy,
		r.validateMinSyncReplicas,
		r.validateMaxSyncReplicas,
		r.validateStorageSize,
		r.validateWalStorageSize,
		r.validateEphemeralVolumeSource,
		r.validateTablespaceStorageSize,
		r.validateName,
		r.validateTablespaceNames,
		r.validateBootstrapPgBaseBackupSource,
		r.validateTablespaceBackupSnapshot,
		r.validateBootstrapRecoverySource,
		r.validateBootstrapRecoveryDataSource,
		r.validateExternalClusters,
		r.validateTolerations,
		r.validateAntiAffinity,
		r.validateReplicaMode,
		r.validateBackupConfiguration,
		r.validateRetentionPolicy,
		r.validateConfiguration,
		r.validateLDAP,
		r.validateReplicationSlots,
		r.validateEnv,
		r.validateManagedServices,
		r.validateManagedRoles,
		r.validateManagedExtensions,
		r.validateResources,
		r.validateHibernationAnnotation,
		r.validatePromotionToken,
	}

	for _, validate := range validations {
		allErrs = append(allErrs, validate()...)
	}

	return allErrs
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	clusterLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	oldCluster := old.(*Cluster)

	// applying defaults before validating updates to set any new default
	oldCluster.SetDefaults()

	allErrs := append(
		r.Validate(),
		r.ValidateChanges(oldCluster)...,
	)

	if len(allErrs) == 0 {
		return r.getAdmissionWarnings(), nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.cnpg.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// ValidateChanges groups the validation logic for cluster changes checking the differences between
// the previous version and the new one of the cluster, returning a list of all encountered errors
func (r *Cluster) ValidateChanges(old *Cluster) (allErrs field.ErrorList) {
	if old == nil {
		clusterLog.Info("Received invalid old object, skipping old object validation",
			"old", old)
		return nil
	}
	type validationFunc func(old *Cluster) field.ErrorList
	validations := []validationFunc{
		r.validateImageChange,
		r.validateConfigurationChange,
		r.validateStorageChange,
		r.validateWalStorageChange,
		r.validateTablespacesChange,
		r.validateUnixPermissionIdentifierChange,
		r.validateReplicationSlotsChange,
		r.validateWALLevelChange,
		r.validateReplicaClusterChange,
	}
	for _, validate := range validations {
		allErrs = append(allErrs, validate(old)...)
	}

	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() (admission.Warnings, error) {
	clusterLog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

// validateLDAP validates the ldap postgres configuration
func (r *Cluster) validateLDAP() field.ErrorList {
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
func (r *Cluster) validateEnv() field.ErrorList {
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
func (r *Cluster) validateInitDB() field.ErrorList {
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
	result = r.validateApplicationDatabase(initDBOptions.Database, initDBOptions.Owner,
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

func (r *Cluster) validateImport() field.ErrorList {
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
	case MicroserviceSnapshotType:
		return importSpec.validateMicroservice()
	case MonolithSnapshotType:
		return importSpec.validateMonolith()
	default:
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "import", "type"),
				importSpec.Type,
				"Unrecognized import type"),
		}
	}
}

func (s Import) validateMicroservice() field.ErrorList {
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

func (s Import) validateMonolith() field.ErrorList {
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
func (r *Cluster) validateRecoveryApplicationDatabase() field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	if r.Spec.Bootstrap.Recovery == nil {
		return result
	}

	recoveryOptions := r.Spec.Bootstrap.Recovery
	return r.validateApplicationDatabase(recoveryOptions.Database, recoveryOptions.Owner,
		"recovery")
}

// validatePgBaseBackup validate the bootstrapping options when pg_basebackup
// method is used
func (r *Cluster) validatePgBaseBackupApplicationDatabase() field.ErrorList {
	var result field.ErrorList

	// If it's not configured, everything is ok
	if r.Spec.Bootstrap == nil {
		return result
	}

	if r.Spec.Bootstrap.PgBaseBackup == nil {
		return result
	}

	pgBaseBackupOptions := r.Spec.Bootstrap.PgBaseBackup
	return r.validateApplicationDatabase(pgBaseBackupOptions.Database, pgBaseBackupOptions.Owner,
		"pg_basebackup")
}

// validateApplicationDatabase validate the configuration for application database
func (r *Cluster) validateApplicationDatabase(
	database string,
	owner string,
	command string,
) field.ErrorList {
	var result field.ErrorList
	// If you specify the database name, then you need also to specify the
	// owner user and vice-versa
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
func (r *Cluster) validateCerts() field.ErrorList {
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
func (r *Cluster) validateSuperuserSecret() field.ErrorList {
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
func (r *Cluster) validateBootstrapMethod() field.ErrorList {
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
			field.Invalid(
				field.NewPath("spec", "bootstrap"),
				"",
				"Too many bootstrap types specified"))
	}

	return result
}

// validateBootstrapPgBaseBackupSource is used to ensure that the source
// server is correctly defined
func (r *Cluster) validateBootstrapPgBaseBackupSource() field.ErrorList {
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
func (r *Cluster) validateBootstrapRecoverySource() field.ErrorList {
	var result field.ErrorList

	// This validation is only applicable for recovery based bootstrap
	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.Recovery == nil || r.Spec.Bootstrap.Recovery.Source == "" {
		return result
	}

	_, found := r.ExternalCluster(r.Spec.Bootstrap.Recovery.Source)
	if !found {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "source"),
				r.Spec.Bootstrap.Recovery.Source,
				fmt.Sprintf("External cluster %v not found", r.Spec.Bootstrap.Recovery.Source)))
	}

	return result
}

// validateBootstrapRecoveryDataSource is used to ensure that the data
// source is correctly defined
func (r *Cluster) validateBootstrapRecoveryDataSource() field.ErrorList {
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
	value v1.TypedLocalObjectReference,
	path *field.Path,
) field.ErrorList {
	apiGroup := ""
	if value.APIGroup != nil {
		apiGroup = *value.APIGroup
	}

	switch {
	case apiGroup == storagesnapshotv1.GroupName && value.Kind == "VolumeSnapshot":
	case apiGroup == v1.GroupName && value.Kind == "PersistentVolumeClaim":
	default:
		return field.ErrorList{
			field.Invalid(path, value, "Only VolumeSnapshots and PersistentVolumeClaims are supported"),
		}
	}

	return nil
}

// validateImageName validates the image name ensuring we aren't
// using the "latest" tag
func (r *Cluster) validateImageName() field.ErrorList {
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
func (r *Cluster) validateImagePullPolicy() field.ErrorList {
	var result field.ErrorList

	switch r.Spec.ImagePullPolicy {
	case v1.PullAlways, v1.PullNever, v1.PullIfNotPresent, "":
		return result
	default:
		return append(
			result,
			field.Invalid(
				field.NewPath("spec", "imagePullPolicy"),
				r.Spec.ImagePullPolicy,
				fmt.Sprintf("invalid imagePullPolicy, if defined must be one of '%s', '%s' or '%s'",
					v1.PullAlways, v1.PullNever, v1.PullIfNotPresent)))
	}
}

func (r *Cluster) validateResources() field.ErrorList {
	var result field.ErrorList

	cpuRequest := r.Spec.Resources.Requests.Cpu()
	cpuLimits := r.Spec.Resources.Limits.Cpu()
	if !cpuRequest.IsZero() && !cpuLimits.IsZero() {
		cpuRequestGtThanLimit := cpuRequest.Cmp(*cpuLimits) > 0
		if cpuRequestGtThanLimit {
			result = append(result, field.Invalid(
				field.NewPath("spec", "resources", "requests", "cpu"),
				cpuRequest.String(),
				"CPU request is greater than the limit",
			))
		}
	}

	memoryRequest := r.Spec.Resources.Requests.Memory()
	rawSharedBuffer := r.Spec.PostgresConfiguration.Parameters[sharedBuffersParameter]
	if !memoryRequest.IsZero() && rawSharedBuffer != "" {
		if sharedBuffers, err := parsePostgresQuantityValue(rawSharedBuffer); err == nil {
			if memoryRequest.Cmp(sharedBuffers) < 0 {
				result = append(result, field.Invalid(
					field.NewPath("spec", "resources", "requests", "memory"),
					memoryRequest.String(),
					"Memory request is lower than PostgreSQL `shared_buffers` value",
				))
			}
		}
	}

	memoryLimits := r.Spec.Resources.Limits.Memory()
	if !memoryRequest.IsZero() && !memoryLimits.IsZero() {
		memoryRequestGtThanLimit := memoryRequest.Cmp(*memoryLimits) > 0
		if memoryRequestGtThanLimit {
			result = append(result, field.Invalid(
				field.NewPath("spec", "resources", "requests", "memory"),
				memoryRequest.String(),
				"Memory request is greater than the limit",
			))
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

// validateConfiguration determines whether a PostgreSQL configuration is valid
func (r *Cluster) validateConfiguration() field.ErrorList {
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

	pgVersion, err := r.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}
	if pgVersion.Major() < 11 {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Unsupported PostgreSQL version. Versions 11 or newer are supported"))
	}
	info := postgres.ConfigurationInfo{
		Settings:               postgres.CnpgConfigurationSettings,
		Version:                pgVersion,
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

	walLogHintsValue, walLogHintsSet := r.Spec.PostgresConfiguration.Parameters[postgres.ParameterWalLogHints]
	if walLogHintsSet {
		walLogHintsActivated, err := postgres.ParsePostgresConfigBoolean(walLogHintsValue)
		if err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", postgres.ParameterWalLogHints),
					walLogHintsValue,
					"invalid `wal_log_hints`. Must be a postgres boolean"))
		}
		if r.Spec.Instances > 1 && !walLogHintsActivated {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", postgres.ParameterWalLogHints),
					r.Spec.PostgresConfiguration.Parameters[postgres.ParameterWalLogHints],
					"`wal_log_hints` must be set to `on` when `instances` > 1"))
		}
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

// validateWalSizeConfiguration verifies that min_wal_size < max_wal_size < wal volume size
func validateWalSizeConfiguration(
	postgresConfig PostgresConfiguration, walVolumeSize *resource.Quantity,
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
func (r *Cluster) validateConfigurationChange(old *Cluster) field.ErrorList {
	var result field.ErrorList

	if old.Spec.ImageName != r.Spec.ImageName {
		diff := utils.CollectDifferencesFromMaps(old.Spec.PostgresConfiguration.Parameters,
			r.Spec.PostgresConfiguration.Parameters)
		if len(diff) > 0 {
			jsonDiff, _ := json.Marshal(diff)
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "imageName"),
					r.Spec.ImageName,
					fmt.Sprintf("Can't change image name and configuration at the same time. "+
						"There are differences in PostgreSQL configuration parameters: %s", jsonDiff)))
			return result
		}
	}

	return result
}

func validateSyncReplicaElectionConstraint(constraints SyncReplicaElectionConstraints) *field.Error {
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
func (r *Cluster) validateImageChange(old *Cluster) field.ErrorList {
	var result field.ErrorList
	var newVersion, oldVersion version.Data
	var err error
	var newImagePath *field.Path
	if r.Spec.ImageCatalogRef != nil {
		newImagePath = field.NewPath("spec", "imageCatalogRef")
	} else {
		newImagePath = field.NewPath("spec", "imageName")
	}

	r.Status.Image = ""
	newVersion, err = r.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}

	old.Status.Image = ""
	oldVersion, err = old.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}

	status := version.IsUpgradePossible(oldVersion, newVersion)

	if !status {
		result = append(
			result,
			field.Invalid(
				newImagePath,
				newVersion,
				fmt.Sprintf("can't upgrade between majors %v and %v",
					oldVersion, newVersion)))
	}

	return result
}

// Validate the recovery target to ensure that the mutual exclusivity
// of options is respected and plus validating the format of targetTime
// if specified
func (r *Cluster) validateRecoveryTarget() field.ErrorList {
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

func validateTargetExclusiveness(recoveryTarget *RecoveryTarget) field.ErrorList {
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
func (r *Cluster) validatePrimaryUpdateStrategy() field.ErrorList {
	if r.Spec.PrimaryUpdateStrategy == "" {
		return nil
	}

	var result field.ErrorList

	if r.Spec.PrimaryUpdateStrategy != PrimaryUpdateStrategySupervised &&
		r.Spec.PrimaryUpdateStrategy != PrimaryUpdateStrategyUnsupervised {
		result = append(result, field.Invalid(
			field.NewPath("spec", "primaryUpdateStrategy"),
			r.Spec.PrimaryUpdateStrategy,
			"primaryUpdateStrategy should be empty, 'supervised' or 'unsupervised'"))
		return result
	}

	if r.Spec.PrimaryUpdateStrategy == PrimaryUpdateStrategySupervised && r.Spec.Instances == 1 {
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
func (r *Cluster) validateMaxSyncReplicas() field.ErrorList {
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
func (r *Cluster) validateMinSyncReplicas() field.ErrorList {
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

func (r *Cluster) validateStorageSize() field.ErrorList {
	return validateStorageConfigurationSize(*field.NewPath("spec", "storage"), r.Spec.StorageConfiguration)
}

func (r *Cluster) validateWalStorageSize() field.ErrorList {
	var result field.ErrorList

	if r.ShouldCreateWalArchiveVolume() {
		result = append(result,
			validateStorageConfigurationSize(*field.NewPath("spec", "walStorage"), *r.Spec.WalStorage)...)
	}

	return result
}

func (r *Cluster) validateEphemeralVolumeSource() field.ErrorList {
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

func (r *Cluster) validateTablespaceStorageSize() field.ErrorList {
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
	storageConfiguration StorageConfiguration,
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
func (r *Cluster) validateStorageChange(old *Cluster) field.ErrorList {
	return validateStorageConfigurationChange(
		field.NewPath("spec", "storage"),
		old.Spec.StorageConfiguration,
		r.Spec.StorageConfiguration,
	)
}

func (r *Cluster) validateWalStorageChange(old *Cluster) field.ErrorList {
	if old.Spec.WalStorage == nil {
		return nil
	}

	if old.Spec.WalStorage != nil && r.Spec.WalStorage == nil {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "walStorage"),
				r.Spec.WalStorage,
				"walStorage cannot be disabled once the cluster is created"),
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
func (r *Cluster) validateTablespacesChange(old *Cluster) field.ErrorList {
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
	oldStorage StorageConfiguration,
	newStorage StorageConfiguration,
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
func (r *Cluster) validateName() field.ErrorList {
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

func (r *Cluster) validateTablespaceNames() field.ErrorList {
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

func (r *Cluster) validateTablespaceBackupSnapshot() field.ErrorList {
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
func (r *Cluster) validateExternalClusters() field.ErrorList {
	var result field.ErrorList
	stringSet := stringset.New()

	for idx, externalCluster := range r.Spec.ExternalClusters {
		path := field.NewPath("spec", "externalClusters").Index(idx)
		stringSet.Put(externalCluster.Name)
		result = append(
			result,
			r.validateExternalCluster(&r.Spec.ExternalClusters[idx], path)...)
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
func (r *Cluster) validateExternalCluster(externalCluster *ExternalCluster, path *field.Path) field.ErrorList {
	var result field.ErrorList

	if externalCluster.ConnectionParameters == nil && externalCluster.BarmanObjectStore == nil {
		result = append(result,
			field.Invalid(
				path,
				externalCluster,
				"one of connectionParameters and barmanObjectStore is required"))
	}

	return result
}

func (r *Cluster) validateReplicaClusterChange(old *Cluster) field.ErrorList {
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

func (r *Cluster) validateUnixPermissionIdentifierChange(old *Cluster) field.ErrorList {
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

func (r *Cluster) validatePromotionToken() field.ErrorList {
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
func (r *Cluster) validateReplicaMode() field.ErrorList {
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
			len(r.ObjectMeta.ResourceVersion) == 0 {
			result = append(result, field.Invalid(
				field.NewPath("spec", "replicaCluster"),
				replicaClusterConf,
				"replica mode bootstrap is compatible only with pg_basebackup or recovery"))
		}
	}

	result = append(result, r.validateReplicaClusterExternalClusters()...)

	return result
}

func (r *Cluster) validateReplicaClusterExternalClusters() field.ErrorList {
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
func (r *Cluster) validateTolerations() field.ErrorList {
	path := field.NewPath("spec", "affinity", "toleration")
	allErrors := field.ErrorList{}
	for i, toleration := range r.Spec.Affinity.Tolerations {
		idxPath := path.Index(i)
		// validate the toleration key
		if len(toleration.Key) > 0 {
			allErrors = append(allErrors, validation.ValidateLabelName(toleration.Key, idxPath.Child("key"))...)
		}

		// empty toleration key with Exists operator and empty value means match all taints
		if len(toleration.Key) == 0 && toleration.Operator != v1.TolerationOpExists {
			allErrors = append(allErrors,
				field.Invalid(idxPath.Child("operator"),
					toleration.Operator,
					"operator must be Exists when `key` is empty, which means \"match all values and all keys\""))
		}

		if toleration.TolerationSeconds != nil && toleration.Effect != v1.TaintEffectNoExecute {
			allErrors = append(allErrors,
				field.Invalid(idxPath.Child("effect"),
					toleration.Effect,
					"effect must be 'NoExecute' when `tolerationSeconds` is set"))
		}

		// validate toleration operator and value
		switch toleration.Operator {
		// empty operator means Equal
		case v1.TolerationOpEqual, "":
			if errs := validationutil.IsValidLabelValue(toleration.Value); len(errs) != 0 {
				allErrors = append(allErrors,
					field.Invalid(idxPath.Child("operator"),
						toleration.Value, strings.Join(errs, ";")))
			}
		case v1.TolerationOpExists:
			if len(toleration.Value) > 0 {
				allErrors = append(allErrors,
					field.Invalid(idxPath.Child("operator"),
						toleration, "value must be empty when `operator` is 'Exists'"))
			}
		default:
			validValues := []string{string(v1.TolerationOpEqual), string(v1.TolerationOpExists)}
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

// validateTaintEffect is used from validateTollerations and is a verbatim copy of the code
// at https://github.com/kubernetes/kubernetes/blob/4d38d21/pkg/apis/core/validation/validation.go#L3087
func validateTaintEffect(effect *v1.TaintEffect, allowEmpty bool, fldPath *field.Path) field.ErrorList {
	if !allowEmpty && len(*effect) == 0 {
		return field.ErrorList{field.Required(fldPath, "")}
	}

	allErrors := field.ErrorList{}
	switch *effect {
	// TODO: Replace next line with subsequent commented-out line when implement TaintEffectNoScheduleNoAdmit.
	case v1.TaintEffectNoSchedule, v1.TaintEffectPreferNoSchedule, v1.TaintEffectNoExecute:
		// case core.TaintEffectNoSchedule, core.TaintEffectPreferNoSchedule, core.TaintEffectNoScheduleNoAdmit,
		//     core.TaintEffectNoExecute:
	default:
		validValues := []string{
			string(v1.TaintEffectNoSchedule),
			string(v1.TaintEffectPreferNoSchedule),
			string(v1.TaintEffectNoExecute),
			// TODO: Uncomment this block when implement TaintEffectNoScheduleNoAdmit.
			// string(core.TaintEffectNoScheduleNoAdmit),
		}
		allErrors = append(allErrors, field.NotSupported(fldPath, *effect, validValues))
	}
	return allErrors
}

// validateAntiAffinity checks and validates the anti-affinity fields.
func (r *Cluster) validateAntiAffinity() field.ErrorList {
	path := field.NewPath("spec", "affinity", "podAntiAffinityType")
	allErrors := field.ErrorList{}

	if r.Spec.Affinity.PodAntiAffinityType != PodAntiAffinityTypePreferred &&
		r.Spec.Affinity.PodAntiAffinityType != PodAntiAffinityTypeRequired &&
		r.Spec.Affinity.PodAntiAffinityType != "" {
		allErrors = append(allErrors, field.Invalid(
			path,
			r.Spec.Affinity.PodAntiAffinityType,
			fmt.Sprintf("pod anti-affinity type must be '%s' (default if empty) or '%s'",
				PodAntiAffinityTypePreferred, PodAntiAffinityTypeRequired),
		))
	}
	return allErrors
}

// validateBackupConfiguration validates the backup configuration
func (r *Cluster) validateBackupConfiguration() field.ErrorList {
	if r.Spec.Backup == nil {
		return nil
	}
	return barmanWebhooks.ValidateBackupConfiguration(
		r.Spec.Backup.BarmanObjectStore,
		field.NewPath("spec", "backup", "barmanObjectStore"),
	)
}

// validateRetentionPolicy validates the retention policy configuration
func (r *Cluster) validateRetentionPolicy() field.ErrorList {
	if r.Spec.Backup == nil {
		return nil
	}
	return barmanWebhooks.ValidateRetentionPolicy(
		r.Spec.Backup.RetentionPolicy,
		field.NewPath("spec", "backup", "retentionPolicy"),
	)
}

func (r *Cluster) validateReplicationSlots() field.ErrorList {
	if r.Spec.ReplicationSlots == nil {
		r.Spec.ReplicationSlots = &ReplicationSlotsConfiguration{
			HighAvailability: &ReplicationSlotsHAConfiguration{
				Enabled: ptr.To(true),
			},
			SynchronizeReplicas: &SynchronizeReplicasConfiguration{
				Enabled: ptr.To(true),
			},
		}
	}
	replicationSlots := r.Spec.ReplicationSlots

	if !replicationSlots.GetEnabled() {
		return nil
	}

	psqlVersion, err := r.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return nil
	}

	if psqlVersion.Major() < 11 {
		if replicationSlots.HighAvailability.GetEnabled() {
			return field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "replicationSlots", "highAvailability", "enabled"),
					replicationSlots.HighAvailability.GetEnabled(),
					"Cannot enable HA replication slots synchronization. PostgreSQL 11 or above required"),
			}
		}

		if replicationSlots.SynchronizeReplicas.GetEnabled() {
			return field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "replicationSlots", "synchronizeReplicas", "enabled"),
					replicationSlots.SynchronizeReplicas.GetEnabled(),
					"Cannot enable user defined replication slots synchronization. PostgreSQL 11 or above required"),
			}
		}
	}

	if errs := r.Spec.ReplicationSlots.SynchronizeReplicas.compileRegex(); len(errs) > 0 {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "replicationSlots", "synchronizeReplicas", "excludePatterns"),
				errs,
				"Cannot configure synchronizeReplicas. Invalid regexes were found"),
		}
	}

	return nil
}

func (r *Cluster) validateReplicationSlotsChange(old *Cluster) field.ErrorList {
	newReplicationSlots := r.Spec.ReplicationSlots
	oldReplicationSlots := old.Spec.ReplicationSlots

	if oldReplicationSlots == nil || oldReplicationSlots.HighAvailability == nil ||
		!oldReplicationSlots.HighAvailability.GetEnabled() {
		return nil
	}

	var errs field.ErrorList

	// when disabling we should check that the prefix it's not removed, and it doesn't change to
	// properly execute the cleanup logic
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

	return errs
}

func (r *Cluster) validateWALLevelChange(old *Cluster) field.ErrorList {
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

func (r *Cluster) validateManagedServices() field.ErrorList {
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

	if slices.Contains(managedServices.DisabledDefaultServices, ServiceSelectorTypeRW) {
		errs = append(errs, field.Invalid(
			basePath.Child("disabledDefaultServices"),
			ServiceSelectorTypeRW,
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
	template ServiceTemplateSpec,
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
func (r *Cluster) validateManagedRoles() field.ErrorList {
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
func (r *Cluster) validateManagedExtensions() field.ErrorList {
	allErrors := field.ErrorList{}

	allErrors = append(allErrors, r.validatePgFailoverSlots()...)
	return allErrors
}

func (r *Cluster) validatePgFailoverSlots() field.ErrorList {
	var result field.ErrorList
	var pgFailoverSlots postgres.ManagedExtension

	for i, ext := range postgres.ManagedExtensions {
		if ext.Name == "pg_failover_slots" {
			pgFailoverSlots = postgres.ManagedExtensions[i]
		}
	}
	if !pgFailoverSlots.IsUsed(r.Spec.PostgresConfiguration.Parameters) {
		return nil
	}

	const hotStandbyFeedbackKey = "hot_standby_feedback"
	hotStandbyFeedbackActivated := false
	hotStandbyFeedback, hasHotStandbyFeedback := r.Spec.PostgresConfiguration.Parameters[hotStandbyFeedbackKey]
	if hasHotStandbyFeedback {
		var err error
		hotStandbyFeedbackActivated, err = postgres.ParsePostgresConfigBoolean(hotStandbyFeedback)
		if err != nil {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", hotStandbyFeedbackKey),
					hotStandbyFeedback,
					fmt.Sprintf("invalid `%s` value. Must be a postgres boolean", hotStandbyFeedbackKey)))
		}
	}

	if !hotStandbyFeedbackActivated {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "postgresql", "parameters", hotStandbyFeedbackKey),
				hotStandbyFeedback,
				fmt.Sprintf("`%s` must be enabled to use %s extension", hotStandbyFeedbackKey, pgFailoverSlots.Name)))
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

func (r *Cluster) getAdmissionWarnings() admission.Warnings {
	return r.getMaintenanceWindowsAdmissionWarnings()
}

func (r *Cluster) getMaintenanceWindowsAdmissionWarnings() admission.Warnings {
	var result admission.Warnings

	if r.Spec.NodeMaintenanceWindow != nil {
		result = append(
			result,
			"Consider using `.spec.enablePDB` instead of the node maintenance window feature")
	}
	return result
}

// validate whether the hibernation configuration is valid
func (r *Cluster) validateHibernationAnnotation() field.ErrorList {
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
