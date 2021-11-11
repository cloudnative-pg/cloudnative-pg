/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/stringset"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// DefaultMonitoringConfigMapKey is the key that should be used in the default metrics configmap to store the queries
const DefaultMonitoringConfigMapKey = "queries"

// clusterLog is for logging in this package.
var clusterLog = log.WithName("cluster-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-k8s-enterprisedb-io-v1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=create;update,versions=v1,name=mcluster.kb.io,sideEffects=None

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	clusterLog.Info("default", "name", r.Name)

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
		r.Spec.PostgresConfiguration.Parameters = postgres.FillCNPConfiguration(
			psqlVersion,
			r.Spec.PostgresConfiguration.Parameters)
	}

	if r.Spec.LogLevel == "" {
		r.Spec.LogLevel = log.InfoLevelString
	}

	// we inject the defaultMonitoringQueries if the MonitoringQueriesConfigmap parameter is not empty
	// and defaultQueries not disabled on cluster crd
	if configuration.Current.MonitoringQueriesConfigmap != "" && !r.Spec.Monitoring.AreDefaultQueriesDisabled() {
		r.defaultMonitoringQueries(configuration.Current.MonitoringQueriesConfigmap)
	}
}

// defaultMonitoringQueries adds the default monitoring queries configMap
// if not already present in CustomQueriesConfigMap
func (r *Cluster) defaultMonitoringQueries(defaultMonitoringQueriesConfigmap string) {
	if r.Spec.Monitoring == nil {
		r.Spec.Monitoring = &MonitoringConfiguration{}
	}

	var defaultConfigMapQueriesAlreadyPresent bool

	// we check if they default queries are been already inserted in the monitoring configuration
	for _, monitoringConfigMap := range r.Spec.Monitoring.CustomQueriesConfigMap {
		if monitoringConfigMap.Name == defaultMonitoringQueriesConfigmap {
			defaultConfigMapQueriesAlreadyPresent = true
			break
		}
	}

	// if the default queries are already present there is no need to re-add them, so we quit the function.
	// Please note that in this case that the default configMap could overwrite user existing queries
	// depending on the order. This is an accepted behavior because the user willingly defined the order of his array
	if defaultConfigMapQueriesAlreadyPresent {
		return
	}

	// we add the default monitoring queries to the array.
	// It is important that the DefaultMonitoringConfigMap is the first element of the array
	// because it should be overwritten by the user defined metrics.
	r.Spec.Monitoring.CustomQueriesConfigMap = append([]ConfigMapKeySelector{
		{
			LocalObjectReference: LocalObjectReference{Name: defaultMonitoringQueriesConfigmap},
			Key:                  DefaultMonitoringConfigMapKey,
		},
	}, r.Spec.Monitoring.CustomQueriesConfigMap...)
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
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,versions=v1,name=vcluster.kb.io,sideEffects=None

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	clusterLog.Info("validate create", "name", r.Name)
	allErrs := r.Validate()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.k8s.enterprisedb.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// Validate groups the validation logic for clusters returning a list of all encountered errors
func (r *Cluster) Validate() (allErrs field.ErrorList) {
	allErrs = append(allErrs, r.validateInitDB()...)
	allErrs = append(allErrs, r.validateSuperuserSecret()...)
	allErrs = append(allErrs, r.validateCerts()...)
	allErrs = append(allErrs, r.validateBootstrapMethod()...)
	allErrs = append(allErrs, r.validateStorageConfiguration()...)
	allErrs = append(allErrs, r.validateImageName()...)
	allErrs = append(allErrs, r.validateImagePullPolicy()...)
	allErrs = append(allErrs, r.validateRecoveryTarget()...)
	allErrs = append(allErrs, r.validatePrimaryUpdateStrategy()...)
	allErrs = append(allErrs, r.validateMinSyncReplicas()...)
	allErrs = append(allErrs, r.validateMaxSyncReplicas()...)
	allErrs = append(allErrs, r.validateStorageSize()...)
	allErrs = append(allErrs, r.validateName()...)
	allErrs = append(allErrs, r.validateBootstrapPgBaseBackupSource()...)
	allErrs = append(allErrs, r.validateBootstrapRecoverySource()...)
	allErrs = append(allErrs, r.validateExternalClusters()...)
	allErrs = append(allErrs, r.validateTolerations()...)
	allErrs = append(allErrs, r.validateAntiAffinity()...)
	allErrs = append(allErrs, r.validateReplicaMode()...)
	allErrs = append(allErrs, r.validateBackupConfiguration()...)

	return allErrs
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	clusterLog.Info("validate update", "name", r.Name)
	oldCluster := old.(*Cluster)

	// applying defaults before validating updates to set any new default
	oldCluster.Default()

	allErrs := append(
		r.Validate(),
		r.ValidateChanges(oldCluster)...,
	)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.k8s.enterprisedb.io", Kind: "Cluster"},
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
	allErrs = append(allErrs, r.validateStorageSizeChange(old)...)
	allErrs = append(allErrs, r.validateReplicaModeChange(old)...)
	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() error {
	clusterLog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
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
	if r.Spec.Bootstrap.PgBaseBackup == nil {
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
	if r.Spec.Bootstrap.Recovery == nil || r.Spec.Bootstrap.Recovery.Source == "" {
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

	psqlVersion, err := r.GetPostgresqlVersion()
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}

	r.Spec.PostgresConfiguration.Parameters = postgres.FillCNPConfiguration(
		psqlVersion,
		r.Spec.PostgresConfiguration.Parameters)
	oldParameters := postgres.FillCNPConfiguration(
		psqlVersion,
		old.Spec.PostgresConfiguration.Parameters)

	for key, value := range r.Spec.PostgresConfiguration.Parameters {
		_, isFixed := postgres.FixedConfigurationParameters[key]
		oldValue, presentInOldConfiguration := oldParameters[key]
		if isFixed && (!presentInOldConfiguration || value != oldValue) {
			result = append(
				result,
				field.Invalid(
					field.NewPath("spec", "postgresql", "parameters", key),
					value,
					"Can't change fixed configuration parameter"))
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
// of options is respected
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

	switch recoveryTarget.TargetTLI {
	case "", "latest", "current":
		// Allowed non numeric values
	default:
		// Everything else must be a valid positive integer
		if tli, err := strconv.Atoi(recoveryTarget.TargetTLI); err != nil || tli < 1 {
			result = append(result, field.Invalid(
				field.NewPath("spec", "bootstrap", "recovery", "recoveryTarget", "targetTLI"),
				recoveryTarget,
				"recovery target timeline can be set to 'latest', 'current' or a positive integer"))
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

// Validate a change in the storage size
func (r *Cluster) validateStorageSizeChange(old *Cluster) field.ErrorList {
	var result field.ErrorList

	oldSize, err := resource.ParseQuantity(old.Spec.StorageConfiguration.Size)
	if err != nil {
		// Can't read the old size, so can't tell if the new size is great
		// or less
		return result
	}

	newSize, err := resource.ParseQuantity(r.Spec.StorageConfiguration.Size)
	if err != nil {
		// Can't read the new size, as this error should already been raised
		// by the size validation
		return result
	}

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

// validateAzureCredentials checks and validates the azure credentials
func (azure *AzureCredentials) validateAzureCredentials(path *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	secrets := 0
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
