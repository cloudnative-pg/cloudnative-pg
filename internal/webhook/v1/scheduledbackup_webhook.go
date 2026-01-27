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
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/robfig/cron"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// scheduledBackupLog is for logging in this package.
var scheduledBackupLog = log.WithName("scheduledbackup-resource").WithValues("version", "v1")

// SetupScheduledBackupWebhookWithManager registers the webhook for ScheduledBackup in the manager.
func SetupScheduledBackupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.ScheduledBackup{}).
		WithValidator(&ScheduledBackupCustomValidator{}).
		WithDefaulter(&ScheduledBackupCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-scheduledbackup,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=create;update,versions=v1,name=mscheduledbackup.cnpg.io,sideEffects=None

// ScheduledBackupCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind ScheduledBackup when those are created or updated.
type ScheduledBackupCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind ScheduledBackup.
func (d *ScheduledBackupCustomDefaulter) Default(_ context.Context, scheduledBackup *apiv1.ScheduledBackup) error {
	scheduledBackupLog.Info("Defaulting for ScheduledBackup",
		"name", scheduledBackup.GetName(), "namespace", scheduledBackup.GetNamespace())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-scheduledbackup,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=scheduledbackups,versions=v1,name=vscheduledbackup.cnpg.io,sideEffects=None

// ScheduledBackupCustomValidator struct is responsible for validating the ScheduledBackup resource
// when it is created, updated, or deleted.
type ScheduledBackupCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ScheduledBackup.
func (v *ScheduledBackupCustomValidator) ValidateCreate(
	_ context.Context,
	scheduledBackup *apiv1.ScheduledBackup,
) (admission.Warnings, error) {
	scheduledBackupLog.Info("Validation for ScheduledBackup upon creation",
		"name", scheduledBackup.GetName(), "namespace", scheduledBackup.GetNamespace())

	warnings, allErrs := v.validate(scheduledBackup)
	if len(allErrs) == 0 {
		return warnings, nil
	}

	return warnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "ScheduledBackup"},
		scheduledBackup.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ScheduledBackup.
func (v *ScheduledBackupCustomValidator) ValidateUpdate(
	_ context.Context,
	_ *apiv1.ScheduledBackup, scheduledBackup *apiv1.ScheduledBackup,
) (admission.Warnings, error) {
	scheduledBackupLog.Info("Validation for ScheduledBackup upon update",
		"name", scheduledBackup.GetName(), "namespace", scheduledBackup.GetNamespace())

	warnings, allErrs := v.validate(scheduledBackup)
	if len(allErrs) == 0 {
		return warnings, nil
	}

	return warnings, apierrors.NewInvalid(
		schema.GroupKind{Group: "scheduledBackup.cnpg.io", Kind: "ScheduledBackup"},
		scheduledBackup.Name, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ScheduledBackup.
func (v *ScheduledBackupCustomValidator) ValidateDelete(
	_ context.Context,
	scheduledBackup *apiv1.ScheduledBackup,
) (admission.Warnings, error) {
	scheduledBackupLog.Info("Validation for ScheduledBackup upon deletion",
		"name", scheduledBackup.GetName(), "namespace", scheduledBackup.GetNamespace())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (v *ScheduledBackupCustomValidator) validate(r *apiv1.ScheduledBackup) (admission.Warnings, field.ErrorList) {
	var result field.ErrorList
	var warnings admission.Warnings

	if _, err := cron.Parse(r.GetSchedule()); err != nil {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "schedule"),
				r.Spec.Schedule, err.Error()))
	} else if len(strings.Fields(r.Spec.Schedule)) != 6 {
		warnings = append(
			warnings,
			"Schedule parameter may not have the right number of arguments "+
				"(usually six arguments are needed)",
		)
	}

	if r.Spec.Method == apiv1.BackupMethodVolumeSnapshot && !utils.HaveVolumeSnapshot() {
		result = append(result, field.Invalid(
			field.NewPath("spec", "method"),
			r.Spec.Method,
			"Cannot use volumeSnapshot backup method due to missing "+
				"VolumeSnapshot CRD. If you installed the CRD after having "+
				"started the operator, please restart it to enable "+
				"VolumeSnapshot support",
		))
	}

	if r.Spec.Method == apiv1.BackupMethodBarmanObjectStore && r.Spec.Online != nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "online"),
			r.Spec.Online,
			"Online parameter can be specified only if the method is volumeSnapshot",
		))
	}

	if r.Spec.Method == apiv1.BackupMethodBarmanObjectStore && r.Spec.OnlineConfiguration != nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "onlineConfiguration"),
			r.Spec.OnlineConfiguration,
			"OnlineConfiguration parameter can be specified only if the method is volumeSnapshot",
		))
	}

	return warnings, result
}
