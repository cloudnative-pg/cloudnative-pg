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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// backupLog is for logging in this package.
var backupLog = log.WithName("backup-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-cnpg-io-v1-backup,mutating=true,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,verbs=create;update,versions=v1,name=mbackup.kb.io,sideEffects=None

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name, "namespace", r.Namespace)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-cnpg-io-v1-backup,mutating=false,failurePolicy=fail,groups=postgresql.cnpg.io,resources=backups,versions=v1,name=vbackup.kb.io,sideEffects=None

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() (admission.Warnings, error) {
	backupLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	backupLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() (admission.Warnings, error) {
	backupLog.Info("validate delete", "name", r.Name, "namespace", r.Namespace)
	return nil, nil
}
