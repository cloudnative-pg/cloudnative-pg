/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// backupLog is for logging in this package.
var backupLog = log.WithName("backup-resource").WithValues("version", "v1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},path=/mutate-postgresql-k8s-enterprisedb-io-v1-backup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=create;update,versions=v1,name=mbackup.kb.io,sideEffects=None

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name, "namespace", r.Namespace)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1},admissionReviewVersions={v1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-backup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,versions=v1,name=vbackup.kb.io,sideEffects=None

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() error {
	backupLog.Info("validate create", "name", r.Name, "namespace", r.Namespace)
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(old runtime.Object) error {
	backupLog.Info("validate update", "name", r.Name, "namespace", r.Namespace)
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() error {
	backupLog.Info("validate delete", "name", r.Name, "namespace", r.Namespace)
	return nil
}
