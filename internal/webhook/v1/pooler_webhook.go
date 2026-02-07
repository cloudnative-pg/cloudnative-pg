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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// AllowedPgbouncerGenericConfigurationParameters is the list of allowed parameters for PgBouncer
var AllowedPgbouncerGenericConfigurationParameters = stringset.From([]string{
	"application_name_add_host",
	"auth_type",
	"autodb_idle_timeout",
	"cancel_wait_timeout",
	"client_idle_timeout",
	"client_login_timeout",
	"client_tls_sslmode",
	"default_pool_size",
	"disable_pqexec",
	"dns_max_ttl",
	"dns_nxdomain_ttl",
	"idle_transaction_timeout",
	"ignore_startup_parameters",
	"listen_backlog",
	"log_connections",
	"log_disconnections",
	"log_pooler_errors",
	"log_stats",
	"max_client_conn",
	"max_db_connections",
	"max_packet_size",
	"max_prepared_statements",
	"max_user_connections",
	"min_pool_size",
	"pkt_buf",
	"query_timeout",
	"query_wait_timeout",
	"reserve_pool_size",
	"reserve_pool_timeout",
	"sbuf_loopcnt",
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
	"server_tls_ciphers",
	"server_tls_protocols",
	"server_tls_sslmode",
	"stats_period",
	"suspend_timeout",
	"tcp_defer_accept",
	"tcp_socket_buffer",
	"tcp_keepalive",
	"tcp_keepcnt",
	"tcp_keepidle",
	"tcp_keepintvl",
	"tcp_user_timeout",
	"track_extra_parameters",
	"verbose",
})

// poolerLog is for logging in this package.
var poolerLog = log.WithName("pooler-resource").WithValues("version", "v1")

// SetupPoolerWebhookWithManager registers the webhook for Pooler in the manager.
func SetupPoolerWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.Pooler{}).
		WithValidator(newBypassableValidator[*apiv1.Pooler](&PoolerCustomValidator{})).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-pooler,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=poolers,versions=v1,name=vpooler.cnpg.io,sideEffects=None

// PoolerCustomValidator struct is responsible for validating the Pooler resource
// when it is created, updated, or deleted.
type PoolerCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Pooler.
func (v *PoolerCustomValidator) ValidateCreate(_ context.Context, pooler *apiv1.Pooler) (admission.Warnings, error) {
	poolerLog.Info("Validation for Pooler upon creation", "name", pooler.GetName(), "namespace", pooler.GetNamespace())

	var warns admission.Warnings
	if !pooler.IsAutomatedIntegration() {
		poolerLog.Info("Pooler not automatically configured, manual configuration required",
			"name", pooler.Name, "namespace", pooler.Namespace, "cluster", pooler.Spec.Cluster.Name)
		warns = append(warns, fmt.Sprintf("The operator won't handle the Pooler %q integration with the Cluster %q (%q). "+
			"Manually configure it as described in the docs.", pooler.Name, pooler.Spec.Cluster.Name, pooler.Namespace))
	}

	warns = append(warns, v.validateDeprecatedMonitoringFields(pooler)...)

	allErrs := v.validate(pooler)

	if len(allErrs) == 0 {
		return warns, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Pooler"},
		pooler.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Pooler.
func (v *PoolerCustomValidator) ValidateUpdate(
	_ context.Context,
	oldPooler *apiv1.Pooler, pooler *apiv1.Pooler,
) (admission.Warnings, error) {
	poolerLog.Info("Validation for Pooler upon update", "name", pooler.GetName(), "namespace", pooler.GetNamespace())

	var warns admission.Warnings
	if oldPooler.IsAutomatedIntegration() && !pooler.IsAutomatedIntegration() {
		poolerLog.Info("Pooler not automatically configured, manual configuration required",
			"name", pooler.Name, "namespace", pooler.Namespace, "cluster", pooler.Spec.Cluster.Name)
		warns = append(warns, fmt.Sprintf("The operator won't handle the Pooler %q integration with the Cluster %q (%q). "+
			"Manually configure it as described in the docs.", pooler.Name, pooler.Spec.Cluster.Name, pooler.Namespace))
	}

	warns = append(warns, v.validateDeprecatedMonitoringFields(pooler)...)

	allErrs := v.validate(pooler)
	if len(allErrs) == 0 {
		return warns, nil
	}

	return warns, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Pooler"},
		pooler.Name, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Pooler.
func (v *PoolerCustomValidator) ValidateDelete(_ context.Context, pooler *apiv1.Pooler) (admission.Warnings, error) {
	poolerLog.Info("Validation for Pooler upon deletion", "name", pooler.GetName(), "namespace", pooler.GetNamespace())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (v *PoolerCustomValidator) validatePgBouncer(r *apiv1.Pooler) field.ErrorList {
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

	if r.Spec.PgBouncer != nil && len(r.Spec.PgBouncer.Parameters) > 0 {
		result = append(result, v.validatePgbouncerGenericParameters(r)...)
	}

	return result
}

func (v *PoolerCustomValidator) validateCluster(r *apiv1.Pooler) field.ErrorList {
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

// validate validates the configuration of a Pooler, returning
// a list of errors
func (v *PoolerCustomValidator) validate(r *apiv1.Pooler) (allErrs field.ErrorList) {
	allErrs = append(allErrs, v.validatePgBouncer(r)...)
	allErrs = append(allErrs, v.validateCluster(r)...)
	return allErrs
}

// validatePgbouncerGenericParameters validates pgbouncer parameters
func (v *PoolerCustomValidator) validatePgbouncerGenericParameters(r *apiv1.Pooler) field.ErrorList {
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

// validateDeprecatedMonitoringFields returns warnings for deprecated monitoring fields
func (v *PoolerCustomValidator) validateDeprecatedMonitoringFields(r *apiv1.Pooler) admission.Warnings {
	var warns admission.Warnings

	//nolint:staticcheck // Checking deprecated fields to warn users
	if r.Spec.Monitoring != nil {
		if r.Spec.Monitoring.EnablePodMonitor ||
			len(r.Spec.Monitoring.PodMonitorMetricRelabelConfigs) > 0 ||
			len(r.Spec.Monitoring.PodMonitorRelabelConfigs) > 0 {
			warns = append(warns, "spec.monitoring is deprecated and will be removed in a future release. "+
				"Set this field to false and create a PodMonitor resource for your pooler as described in the documentation")
		}
	}

	return warns
}
