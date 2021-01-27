/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"github.com/robfig/cron"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// scheduledBackupLog is for logging in this package.
var scheduledBackupLog = logf.Log.WithName("scheduledbackup-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *ScheduledBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1-scheduledbackup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,verbs=create;update,versions=v1,name=mscheduledbackup.kb.io,sideEffects=None

var _ webhook.Defaulter = &ScheduledBackup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *ScheduledBackup) Default() {
	scheduledBackupLog.Info("default", "name", r.Name)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-scheduledbackup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,versions=v1,name=vscheduledbackup.kb.io,sideEffects=None

var _ webhook.Validator = &ScheduledBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateCreate() error {
	var allErrs field.ErrorList
	scheduledBackupLog.Info("validate create", "name", r.Name)

	allErrs = append(allErrs, r.validateSchedule()...)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "scheduledbackup.k8s.enterprisedb.io", Kind: "Backup"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateUpdate(old runtime.Object) error {
	scheduledBackupLog.Info("validate update", "name", r.Name)
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateDelete() error {
	scheduledBackupLog.Info("validate delete", "name", r.Name)
	return nil
}

func (r *ScheduledBackup) validateSchedule() field.ErrorList {
	var result field.ErrorList

	if _, err := cron.Parse(r.GetSchedule()); err != nil {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "schedule"),
				r.Spec.Schedule, err.Error()))
	}

	return result
}
