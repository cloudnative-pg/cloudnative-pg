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
	"strconv"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// backupLog is for logging in this package.
var backupLog = log.WithName("backup-resource").WithValues("version", "v1")

// SetupBackupWebhookWithManager registers the webhook for Backup in the manager.
func SetupBackupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1.Backup{}).
		WithValidator(newBypassableValidator[*apiv1.Backup](&BackupCustomValidator{})).
		WithDefaulter(&BackupCustomDefaulter{}).
		Complete()
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-backup,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,verbs=create;update,versions=v1,name=mbackup.cnpg.io,sideEffects=None

// BackupCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Backup when those are created or updated.
type BackupCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Backup.
func (d *BackupCustomDefaulter) Default(_ context.Context, backup *apiv1.Backup) error {
	backupLog.Info("Defaulting for Backup", "name", backup.GetName(), "namespace", backup.GetNamespace())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-backup,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,versions=v1,name=vbackup.cnpg.io,sideEffects=None

// BackupCustomValidator struct is responsible for validating the Backup resource
// when it is created, updated, or deleted.
type BackupCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Backup.
func (v *BackupCustomValidator) ValidateCreate(_ context.Context, backup *apiv1.Backup) (admission.Warnings, error) {
	backupLog.Info("Validation for Backup upon creation", "name", backup.GetName(), "namespace", backup.GetNamespace())

	allErrs := v.validate(backup)
	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Backup"},
		backup.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Backup.
func (v *BackupCustomValidator) ValidateUpdate(
	_ context.Context,
	_ *apiv1.Backup, backup *apiv1.Backup,
) (admission.Warnings, error) {
	backupLog.Info("Validation for Backup upon update", "name", backup.GetName(), "namespace", backup.GetNamespace())

	allErrs := v.validate(backup)
	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Backup"},
		backup.Name, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Backup.
func (v *BackupCustomValidator) ValidateDelete(_ context.Context, backup *apiv1.Backup) (admission.Warnings, error) {
	backupLog.Info("Validation for Backup upon deletion", "name", backup.GetName(), "namespace", backup.GetNamespace())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (v *BackupCustomValidator) validate(r *apiv1.Backup) field.ErrorList {
	var result field.ErrorList

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
			"Online parameter can be specified only if the backup method is volumeSnapshot",
		))
	}

	if r.Spec.Method == apiv1.BackupMethodBarmanObjectStore && r.Spec.OnlineConfiguration != nil {
		result = append(result, field.Invalid(
			field.NewPath("spec", "onlineConfiguration"),
			r.Spec.OnlineConfiguration,
			"OnlineConfiguration parameter can be specified only if the backup method is volumeSnapshot",
		))
	}

	if r.Spec.Method == apiv1.BackupMethodPlugin && r.Spec.PluginConfiguration.IsEmpty() {
		result = append(result, field.Invalid(
			field.NewPath("spec", "pluginConfiguration"),
			r.Spec.OnlineConfiguration,
			"cannot be empty when the backup method is plugin",
		))
	}

	if value := r.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName]; value != "" {
		_, err := strconv.Atoi(value)
		if err != nil {
			result = append(result, field.Invalid(
				field.NewPath("metadata", "annotations", utils.BackupVolumeSnapshotDeadlineAnnotationName),
				value,
				"must be an integer",
			))
		}
	}

	return result
}
