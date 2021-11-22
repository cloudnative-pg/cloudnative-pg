/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/stringset"
)

var (
	// poolerLog is for logging in this package.
	poolerLog = log.WithName("pooler-resource").WithValues("version", "v1")

	// AllowedPgbouncerGenericConfigurationParameters is the list of allowed parameters for PgBouncer
	AllowedPgbouncerGenericConfigurationParameters = stringset.From([]string{
		"application_name_add_host",
		"autodb_idle_timeout",
		"client_idle_timeout",
		"client_login_timeout",
		"default_pool_size",
		"disable_pqexec",
		"idle_transaction_timeout",
		"ignore_startup_parameters",
		"log_connections",
		"log_disconnections",
		"log_pooler_errors",
		"log_stats",
		"max_client_conn",
		"max_db_connections",
		"max_user_connections",
		"min_pool_size",
		"query_timeout",
		"query_wait_timeout",
		"reserve_pool_size",
		"reserve_pool_timeout",
		"server_check_delay",
		"server_check_query",
		"server_connect_timeout",
		"server_fast_close",
		"server_idle_timeout",
		"server_lifetime",
		"server_login_retry",
		"server_reset_query",
		"server_reset_query_always",
		"server_round_robin",
		"stats_period",
		"verbose",
	})
)

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Pooler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-pooler,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=poolers,versions=v1,name=vpooler.kb.io,sideEffects=None

var _ webhook.Validator = &Pooler{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Pooler) ValidateCreate() error {
	var allErrs field.ErrorList
	poolerLog.Info("validate create", "name", r.Name)

	allErrs = r.Validate()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.k8s.enterprisedb.io", Kind: "Pooler"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Pooler) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	poolerLog.Info("validate update", "name", r.Name)

	allErrs = r.Validate()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.k8s.enterprisedb.io", Kind: "Pooler"},
		r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Pooler) ValidateDelete() error {
	poolerLog.Info("validate delete", "name", r.Name)
	return nil
}

func (r *Pooler) validatePgBouncer() field.ErrorList {
	var result field.ErrorList
	switch {
	case r.Spec.PgBouncer == nil:
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "pgbouncer"),
				"", "required pgbouncer configuration"))
	case r.Spec.PgBouncer.AuthQuerySecret != nil && r.Spec.PgBouncer.AuthQuerySecret.Name != "" &&
		r.Spec.PgBouncer.AuthQuery == "":
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "pgbouncer", "authQuery"),
				"", "must specify an auth query when providing an auth query secret"))
	case (r.Spec.PgBouncer.AuthQuerySecret == nil || r.Spec.PgBouncer.AuthQuerySecret.Name == "") &&
		r.Spec.PgBouncer.AuthQuery != "":
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "pgbouncer", "authQuerySecret", "name"),
				"", "must specify an existing auth query secret when providing an auth query secret"))
	}

	result = append(result, r.validatePgbouncerGenericParameters()...)

	return result
}

func (r *Pooler) validateCluster() field.ErrorList {
	var result field.ErrorList
	if r.Spec.Cluster.Name == "" {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "cluster", "name"),
				"", "must specify a cluster name"))
	}
	if r.Spec.Cluster.Name == r.Name {
		result = append(result,
			field.Invalid(
				field.NewPath("metadata", "name"),
				r.Name, "the pooler resource cannot have the same name of a cluster"))
	}
	return result
}

// Validate validates the configuration of a Pooler, returning
// a list of errors
func (r *Pooler) Validate() (allErrs field.ErrorList) {
	allErrs = append(allErrs, r.validatePgBouncer()...)
	allErrs = append(allErrs, r.validateCluster()...)
	return allErrs
}

// validatePgbouncerGenericParameters validates pgbouncer parameters
func (r *Pooler) validatePgbouncerGenericParameters() field.ErrorList {
	var result field.ErrorList

	for param := range r.Spec.PgBouncer.Parameters {
		if !AllowedPgbouncerGenericConfigurationParameters.Has(param) {
			result = append(result,
				field.Invalid(
					field.NewPath("spec", "cluster", "parameters"),
					param, "Invalid or reserved parameter"))
		}
	}
	return result
}
