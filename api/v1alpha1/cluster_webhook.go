/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/versions"
)

// log is for logging in this package.
var log = logf.Log.WithName("cluster-resource")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-postgresql-k8s-enterprisedb-io-v1alpha1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=create;update,versions=v1alpha1,name=mcluster.kb.io

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	log.Info("default", "name", r.Name)

	// Defaulting the image name if not specified
	if r.Spec.ImageName == "" {
		r.Spec.ImageName = versions.GetDefaultImageName()
	}

	// Defaulting the bootstrap method if not specified
	if r.Spec.Bootstrap == nil {
		r.Spec.Bootstrap = &BootstrapConfiguration{}
	}

	if r.Spec.Bootstrap.InitDB == nil && r.Spec.Bootstrap.FullRecovery == nil {
		r.Spec.Bootstrap.InitDB = &BootstrapInitDB{
			Database: "app",
			Owner:    "app",
		}
	}

	if r.Spec.Bootstrap.InitDB != nil {
		if r.Spec.Bootstrap.InitDB.Database == "" {
			r.Spec.Bootstrap.InitDB.Database = "app"
		}
		if r.Spec.Bootstrap.InitDB.Owner == "" {
			r.Spec.Bootstrap.InitDB.Owner = r.Spec.Bootstrap.InitDB.Database
		}
	}

	imageName := r.GetImageName()
	tag := utils.GetImageTag(imageName)
	psqlVersion, err := postgres.GetPostgresVersionFromTag(tag)
	if err == nil {
		// The validation error will be already raised by the
		// validateImageName function
		r.Spec.PostgresConfiguration.Parameters = postgres.CreateCNPConfiguration(
			psqlVersion, r.Spec.PostgresConfiguration.Parameters, false)
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1alpha1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,versions=v1alpha1,name=vcluster.kb.io

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	var allErrs field.ErrorList
	log.Info("validate create", "name", r.Name)

	allErrs = append(allErrs, r.validateInitDB()...)
	allErrs = append(allErrs, r.validateSuperuserSecret()...)
	allErrs = append(allErrs, r.validateBootstrapMethod()...)
	allErrs = append(allErrs, r.validateStorageConfiguration()...)
	allErrs = append(allErrs, r.validateImageName()...)
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.k8s.enterprisedb.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	log.Info("validate update", "name", r.Name)

	allErrs = append(allErrs, r.validateInitDB()...)
	allErrs = append(allErrs, r.validateSuperuserSecret()...)
	allErrs = append(allErrs, r.validateBootstrapMethod()...)
	allErrs = append(allErrs, r.validateStorageConfiguration()...)
	allErrs = append(allErrs, r.validateImageName()...)

	oldObject := old.(*Cluster)
	if oldObject == nil {
		log.Info("Received invalid old object, skipping old object validation",
			"old", old)
	} else {
		allErrs = append(allErrs, r.validateImageChange(oldObject.Spec.ImageName)...)
		allErrs = append(allErrs, r.validateConfigurationChange(oldObject)...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.k8s.enterprisedb.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() error {
	log.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

// ValidateInitDB validate the bootstrapping options when initdb
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
	if r.Spec.Bootstrap.FullRecovery != nil {
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

// validateImageName validate the image name ensuring we aren't
// using the "latest" tag
func (r *Cluster) validateImageName() field.ErrorList {
	var result field.ErrorList

	if r.Spec.ImageName == "" {
		// We'll use the default one
		return result
	}

	tag := utils.GetImageTag(r.Spec.ImageName)
	if tag == "latest" {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Can't use 'latest' as image tag as we can't detect upgrades"))
	} else {
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

// validateConfigurationChange determine whether a PostgreSQL configuration
// change can be applied
func (r *Cluster) validateConfigurationChange(old *Cluster) field.ErrorList {
	var result field.ErrorList

	configChanged := !reflect.DeepEqual(
		old.Spec.PostgresConfiguration.Parameters,
		r.Spec.PostgresConfiguration.Parameters)

	if old.Spec.ImageName != r.Spec.ImageName && configChanged {
		result = append(
			result,
			field.Invalid(
				field.NewPath("spec", "imageName"),
				r.Spec.ImageName,
				"Can't change image name and configuration at the same time"))
		return result
	}

	imageName := r.GetImageName()
	tag := utils.GetImageTag(imageName)
	psqlVersion, err := postgres.GetPostgresVersionFromTag(tag)
	if err != nil {
		// The validation error will be already raised by the
		// validateImageName function
		return result
	}

	r.Spec.PostgresConfiguration.Parameters = postgres.CreateCNPConfiguration(
		psqlVersion, r.Spec.PostgresConfiguration.Parameters, false)
	oldParameters := postgres.CreateCNPConfiguration(
		psqlVersion, old.Spec.PostgresConfiguration.Parameters, false)

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
		newVersion = versions.GetDefaultImageName()
	}

	if old == "" {
		old = versions.GetDefaultImageName()
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
