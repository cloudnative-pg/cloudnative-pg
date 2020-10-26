/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var log = logf.Log.WithName("cluster-resource")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-postgresql-k8s-2ndq-io-v1alpha1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.k8s.2ndq.io,resources=clusters,verbs=create;update,versions=v1alpha1,name=mcluster.kb.io

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	log.Info("default", "name", r.Name)

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
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-postgresql-k8s-2ndq-io-v1alpha1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.k8s.2ndq.io,resources=clusters,versions=v1alpha1,name=vcluster.kb.io

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	var allErrs field.ErrorList
	log.Info("validate create", "name", r.Name)

	allErrs = append(allErrs, r.validateInitDB()...)
	allErrs = append(allErrs, r.validateSuperuserSecret()...)
	allErrs = append(allErrs, r.validateBootstrapMethod()...)
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.k8s.2ndq.io", Kind: "Cluster"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	log.Info("validate update", "name", r.Name)

	allErrs = append(allErrs, r.validateInitDB()...)
	allErrs = append(allErrs, r.validateSuperuserSecret()...)
	allErrs = append(allErrs, r.validateBootstrapMethod()...)
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.k8s.2ndq.io", Kind: "Cluster"},
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
