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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// backupLog is for logging in this package.
var backupLog = log.WithName("backup-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-backup,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,verbs=create;update,versions=v1,name=mbackup.cnpg.io,sideEffects=None

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name, "namespace", r.Namespace)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-backup,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,versions=v1,name=vbackup.cnpg.io,sideEffects=None

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() (admission.Warnings, error) {
	backupLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)
	allErrs := r.validate()
	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Backup"},
		r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	backupLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	return r.ValidateCreate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() (admission.Warnings, error) {
	backupLog.Info("validate delete", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
}

func (r *Backup) validate() field.ErrorList {
	var result field.ErrorList

	if r.Spec.Method == BackupMethodVolumeSnapshot && !utils.HaveVolumeSnapshot() {
		result = append(result, field.Invalid(
			field.NewPath("spec", "method"),
			r.Spec.Method,
			"Cannot use volumeSnapshot backup method due to missing "+
				"VolumeSnapshot CRD. If you installed the CRD after having "+
				"started the operator, please restart it to enable "+
				"VolumeSnapshot support",
		))
	}

	if r.Spec.Method == BackupMethodBarmanObjectStore && r.Spec.Online {
		result = append(result,
			field.Invalid(
				field.NewPath("spec", "method"),
				r.Spec.Method,
				"The online value can be set only for volumeSnapshot method",
			))
	}

	return result
}
