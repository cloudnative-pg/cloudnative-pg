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
	"github.com/robfig/cron"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// scheduledBackupLog is for logging in this package.
var scheduledBackupLog = log.WithName("scheduledbackup-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *ScheduledBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-scheduledbackup,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=create;update,versions=v1,name=mscheduledbackup.kb.io,sideEffects=None

var _ webhook.Defaulter = &ScheduledBackup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *ScheduledBackup) Default() {
	scheduledBackupLog.Info("default", "name", r.Name, "namespace", r.Namespace)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-scheduledbackup,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=scheduledbackups,versions=v1,name=vscheduledbackup.kb.io,sideEffects=None

var _ webhook.Validator = &ScheduledBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateCreate() (admission.Warnings, error) {
	var allErrs field.ErrorList
	scheduledBackupLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)

	allErrs = append(allErrs, r.validateSchedule()...)

	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "scheduledbackup.cnpg.io", Kind: "Backup"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	scheduledBackupLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateDelete() (admission.Warnings, error) {
	scheduledBackupLog.Info("validate delete", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
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
