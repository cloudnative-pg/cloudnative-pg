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
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutil "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/stringset"
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
)

// clusterLog is for logging in this package.
var clusterLog = log.WithName("cluster-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,verbs=create;update,versions=v1,name=mcluster.kb.io,sideEffects=None

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
	if r.Spec.ImageName == "" {
		r.Spec.ImageName = configuration.Current.PostgresImageName
	}

	// Defaulting the bootstrap method if not specified
	if r.Spec.Bootstrap == nil {
		r.Spec.Bootstrap = &BootstrapConfiguration{}
	}

	// Defaulting initDB if no other boostrap method was passed
	if r.Spec.Bootstrap.Recovery == nil && r.Spec.Bootstrap.PgBaseBackup == nil {
		r.defaultInitDB()
	}

	// Defaulting the pod anti-affinity type if podAntiAffinity
	if (r.Spec.Affinity.EnablePodAntiAffinity == nil || *r.Spec.Affinity.EnablePodAntiAffinity) &&
		r.Spec.Affinity.PodAntiAffinityType == "" {
		r.Spec.Affinity.PodAntiAffinityType = PodAntiAffinityTypePreferred
	}

	psqlVersion, err := r.GetPostgresqlVersion()
	if err == nil {
		// The validation error will be already raised by the
		// validateImageName function
		info := postgres.ConfigurationInfo{
			Settings:                      postgres.CnpgConfigurationSettings,
			MajorVersion:                  psqlVersion,
			UserSettings:                  r.Spec.PostgresConfiguration.Parameters,
			IsReplicaCluster:              r.IsReplica(),
			PreserveFixedSettingsFromUser: preserveUserSettings,
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
			Database: "app",
			Owner:    "app",
		}
	}

	if r.Spec.Bootstrap.InitDB.Database == "" {
		r.Spec.Bootstrap.InitDB.Database = "app"
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

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=clusters,versions=v1,name=vcluster.kb.io,sideEffects=None

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	clusterLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)
	allErrs := r.Validate()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// Validate groups the validation logic for clusters returning a list of all encountered errors
func (r *Cluster) Validate() (allErrs field.ErrorList) {
	type validation func() field.ErrorList
	validations := []validation{
		r.validateInitDB,
		r.validateSuperuserSecret,
		r.validateCerts,
		r.validateBootstrapMethod,
		r.validateStorageConfiguration,
		r.validateImageName,
		r.validateImagePullPolicy,
		r.validateRecoveryTarget,
		r.validatePrimaryUpdateStrategy,
		r.validateMinSyncReplicas,
		r.validateMaxSyncReplicas,
		r.validateStorageSize,
		r.validateName,
		r.validateBootstrapPgBaseBackupSource,
		r.validateBootstrapRecoverySource,
		r.validateRecoveryAndBackupTarget,
		r.validateExternalClusters,
		r.validateTolerations,
		r.validateAntiAffinity,
		r.validateReplicaMode,
		r.validateBackupConfiguration,
		r.validateConfiguration,
		r.validateLDAP,
	}

	for _, validate := range validations {
		allErrs = append(allErrs, validate()...)
	}

	return allErrs
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	clusterLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	oldCluster := old.(*Cluster)

	// applying defaults before validating updates to set any new default
	oldCluster.SetDefaults()

	allErrs := append(
		r.Validate(),
		r.ValidateChanges(oldCluster)...,
	)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
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
	allErrs = append(allErrs, r.validateImageChange(old.Spec.ImageName)...)
	allErrs = append(allErrs, r.validateConfigurationChange(old)...)
	allErrs = append(allErrs, r.validateStorageChange(old)...)
	allErrs = append(allErrs, r.validateReplicaModeChange(old)...)
	allErrs = append(allErrs, r.validateUnixPermissionIdentifierChange(old)...)
	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() error {
	clusterLog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
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

	if initDBOptions.Database != "" && initDBOptions.Owner == "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "owner"),
				"",
				"You need to specify the database owner user"))
	}
	if initDBOptions.Database == "" && initDBOptions.Owner != "" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "database"),
				"",
				"You need to specify the database name"))
	}

	if initDBOptions.WalSegmentSize != 0 && !utils.IsPowerOfTwo(initDBOptions.WalSegmentSize) {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "bootstrap", "initdb", "walSegmentSize"),
				initDBOptions.WalSegmentSize,
				"WAL segment size must be a power of 2"))
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

// validateStorageConfiguration validates the size format it's correct
func (r *Cluster) validateStorageConfiguration() field.ErrorList {
	var result field.ErrorList

	if _, err := resource.ParseQuantity(r.Spec.StorageConfiguration.Size); err != nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "storage", "size"),
				r.Spec.StorageConfiguration.Size,
				"Size value isn't valid"))
	}

	return result
}

// validateImageName validates the image name ensuring we aren't
// using the "latest" tag
func (r *Cluster) validateImageName() field.ErrorList {
	var result field.ErrorList

	if r.Spec.ImageName == "" {
		// We'll use the default one
		return result
	}

	tag := utils.GetImageTag(r.Spec.ImageName)
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
		_, err := postgres.GetPostgresVersionFromTag(tag)
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

// validateConfiguration determines whether a PostgreSQL configuration is valid
func (r *Cluster) validateConfiguration() field.ErrorList {
	var result field.ErrorList

	psqlVersion, err := r.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}
	info := postgres.ConfigurationInfo{
		Settings:         postgres.CnpgConfigurationSettings,
		MajorVersion:     psqlVersion,
		UserSettings:     r.Spec.PostgresConfiguration.Parameters,
		IsReplicaCluster: r.IsReplica(),
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

	return result
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

// validateImageChange validate the change from a certain image name
// to a new one.
func (r *Cluster) validateImageChange(old string) field.ErrorList {
	var result field.ErrorList

	newVersion := r.Spec.ImageName
	if newVersion == "" {
		// We'll use the default one
		newVersion = configuration.Current.PostgresImageName
	}

	if old == "" {
		old = configuration.Current.PostgresImageName
	}

	status, err := postgres.CanUpgrade(old, newVersion)
	if err != nil {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				fmt.Sprintf("wrong version: %v", err.Error())))
	} else if !status {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				fmt.Sprintf("can't upgrade between %v and %v",
					old, newVersion)))
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

	// validate format of TargetTime
	if recoveryTarget.TargetTime != "" {
		if _, err := utils.ParseTargetTime(nil, recoveryTarget.TargetTime); err != nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
				recoveryTarget.TargetTime,
				"The format of TargetTime is invalid"))
		}
	}

	// validate TargetLSN
	if recoveryTarget.TargetLSN != "" {
		if _, err := postgres.LSN(recoveryTarget.TargetLSN).Parse(); err != nil {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget"),
				recoveryTarget.TargetLSN,
				"Invalid TargetLSN"))
		}
	}

	switch recoveryTarget.TargetTLI {
	case "", "latest":
		// Allowed non numeric values
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

// Validate if the storage size is a parsable quantity
func (r *Cluster) validateStorageSize() field.ErrorList {
	var result field.ErrorList

	_, err := resource.ParseQuantity(r.Spec.StorageConfiguration.Size)
	if err != nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "storage", "size"),
			r.Spec.StorageConfiguration.Size,
			err.Error()))
	}

	return result
}

// Validate a change in the storage
func (r *Cluster) validateStorageChange(old *Cluster) field.ErrorList {
	var result field.ErrorList

	oldSize, err := resource.ParseQuantity(old.Spec.StorageConfiguration.Size)
	if err != nil {
		// Can't read the old size, so can't tell if the new size is greater
		// or less
		return result
	}

	result = append(result, r.validateStorageSize()...)
	if len(result) != 0 {
		return result
	}
	newSize, _ := resource.ParseQuantity(r.Spec.StorageConfiguration.Size)

	if oldSize.AsDec().Cmp(newSize.AsDec()) == 1 {
		result = append(result, field.Invalid(
			field.NewPath("spec", "storage", "size"),
			r.Spec.StorageConfiguration.Size,
			fmt.Sprintf(
				"can't shrink existing storage from %v to %v",
				old.Spec.StorageConfiguration.Size,
				r.Spec.StorageConfiguration.Size)))
	}

	return result
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

// Check replica mode is enabled only at cluster creation time
func (r *Cluster) validateReplicaModeChange(old *Cluster) field.ErrorList {
	var result field.ErrorList
	// if we are not specifying any replica cluster configuration or disabling it, nothing to do
	if r.Spec.ReplicaCluster == nil || !r.Spec.ReplicaCluster.Enabled {
		return result
	}

	// otherwise if it was not defined before or it was just not enabled, add an error
	if old.Spec.ReplicaCluster == nil || !old.Spec.ReplicaCluster.Enabled {
		result = append(result, field.Invalid(
			field.NewPath("spec", "replicaCluster"),
			r.Spec.ReplicaCluster,
			"Can not enable replication on existing clusters"))
	}

	return result
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

// Check if the replica mode is used with an incompatible bootstrap
// method
func (r *Cluster) validateReplicaMode() field.ErrorList {
	var result field.ErrorList

	if r.Spec.ReplicaCluster == nil {
		return result
	}

	if r.Spec.Bootstrap == nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "bootstrap"),
			r.Spec.ReplicaCluster,
			"bootstrap configuration is required for replica mode"))
	} else if r.Spec.Bootstrap.PgBaseBackup == nil && r.Spec.Bootstrap.Recovery == nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "replicaCluster"),
			r.Spec.ReplicaCluster,
			"replica mode is compatible only with bootstrap using pg_basebackup or recovery"))
	}

	_, found := r.ExternalCluster(r.Spec.ReplicaCluster.Source)
	if !found {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "replicaCluster", "primaryServerName"),
				r.Spec.ReplicaCluster.Source,
				fmt.Sprintf("External cluster %v not found", r.Spec.ReplicaCluster.Source)))
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
	allErrors := field.ErrorList{}

	if r.Spec.Backup == nil || r.Spec.Backup.BarmanObjectStore == nil {
		return nil
	}

	credentialsCount := 0
	if r.Spec.Backup.BarmanObjectStore.AzureCredentials != nil {
		credentialsCount++
		allErrors = r.Spec.Backup.BarmanObjectStore.AzureCredentials.validateAzureCredentials(
			field.NewPath("spec", "backupConfiguration", "azureCredentials"))
	}
	if r.Spec.Backup.BarmanObjectStore.S3Credentials != nil {
		credentialsCount++
		allErrors = r.Spec.Backup.BarmanObjectStore.S3Credentials.validateAwsCredentials(
			field.NewPath("spec", "backupConfiguration", "s3Credentials"))
	}
	if r.Spec.Backup.BarmanObjectStore.GoogleCredentials != nil {
		credentialsCount++
		allErrors = r.Spec.Backup.BarmanObjectStore.GoogleCredentials.validateGCSCredentials(
			field.NewPath("spec", "backupConfiguration", "googleCredentials"))
	}
	if credentialsCount != 1 {
		allErrors = append(allErrors, field.Invalid(
			field.NewPath("spec", "backupConfiguration"),
			r.Spec.Backup.BarmanObjectStore,
			"one and only one of azureCredentials and s3Credentials are required",
		))
	}

	if r.Spec.Backup.RetentionPolicy != "" {
		_, err := utils.ParsePolicy(r.Spec.Backup.RetentionPolicy)
		if err != nil {
			allErrors = append(allErrors, field.Invalid(
				field.NewPath("spec", "retentionPolicy"),
				r.Spec.Backup.RetentionPolicy,
				"not a valid retention policy",
			))
		}
	}

	return allErrors
}

// validateRecoveryAndBackupTarget validates that the recovery point and
// the backup point are not the same
func (r *Cluster) validateRecoveryAndBackupTarget() field.ErrorList {
	allErrors := field.ErrorList{}

	if r.Spec.Bootstrap == nil || r.Spec.Bootstrap.Recovery == nil || r.Spec.Bootstrap.Recovery.Source == "" ||
		r.Spec.Backup == nil || r.Spec.Backup.BarmanObjectStore == nil {
		return allErrors
	}

	var sourceCluster *ExternalCluster
	for i, cluster := range r.Spec.ExternalClusters {
		if cluster.Name == r.Spec.Bootstrap.Recovery.Source {
			sourceCluster = &r.Spec.ExternalClusters[i]
		}
	}

	if sourceCluster == nil || sourceCluster.BarmanObjectStore == nil {
		return allErrors
	}

	barmanObjectStore := r.Spec.Backup.BarmanObjectStore
	sourceBarmanObjectStore := sourceCluster.BarmanObjectStore

	if barmanObjectStore.ServerName == sourceBarmanObjectStore.ServerName &&
		barmanObjectStore.EndpointURL == sourceBarmanObjectStore.EndpointURL &&
		barmanObjectStore.DestinationPath == sourceBarmanObjectStore.DestinationPath {
		allErrors = append(
			allErrors,
			field.Invalid(
				field.NewPath("spec", "backup", "barmanObjectStore"),
				"",
				"Cannot be equal to the ExternalCluster used to recover from"))
	}

	return allErrors
}

// validateAzureCredentials checks and validates the azure credentials
func (azure *AzureCredentials) validateAzureCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	secrets := 0
	if azure.InheritFromAzureAD {
		secrets++
	}
	if azure.StorageKey != nil {
		secrets++
	}
	if azure.StorageSasToken != nil {
		secrets++
	}

	if secrets != 1 && azure.ConnectionString == nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				azure,
				"when connection string is not specified, one and only one of "+
					"storage key and storage SAS token is allowed"))
	}

	if secrets != 0 && azure.ConnectionString != nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				azure,
				"when connection string is specified, the other parameters "+
					"must be empty"))
	}

	return allErrors
}

func (s3 *S3Credentials) validateAwsCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}
	credentials := 0

	if s3.InheritFromIAMRole {
		credentials++
	}
	if s3.AccessKeyIDReference != nil && s3.SecretAccessKeyReference != nil {
		credentials++
	} else if s3.AccessKeyIDReference != nil || s3.SecretAccessKeyReference != nil {
		credentials++
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"when using AWS credentials both accessKeyId and secretAccessKey must be provided",
			),
		)
	}

	if credentials == 0 {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"at least one AWS authentication method should be supplied",
			),
		)
	}

	if credentials > 1 {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				s3,
				"only one AWS authentication method should be supplied",
			),
		)
	}

	return allErrors
}

func (gcs *GoogleCredentials) validateGCSCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	if !gcs.GKEEnvironment && gcs.ApplicationCredentials == nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				gcs,
				"if gkeEnvironment is false, secret with credentials must be provided",
			))
	}

	if gcs.GKEEnvironment && gcs.ApplicationCredentials != nil {
		allErrors = append(
			allErrors,
			field.Invalid(
				path,
				gcs,
				"if gkeEnvironment is true, secret with credentials must not be provided",
			))
	}

	return allErrors
}
