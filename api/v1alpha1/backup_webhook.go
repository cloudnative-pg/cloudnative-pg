/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// backupLog is for logging in this package.
var backupLog = logf.Log.WithName("backup-resource")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1alpha1-backup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=create;update,versions=v1alpha1,name=mwebhook-backup.kb.io

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name)
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create,path=/validate-postgresql-k8s-enterprisedb-io-v1alpha1-backup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,versions=v1alpha1,name=vwebhook-backup.kb.io

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() error {
	backupLog.Info("validate create", "name", r.Name)
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(old runtime.Object) error {
	backupLog.Info("validate update", "name", r.Name)
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() error {
	backupLog.Info("validate delete", "name", r.Name)
	return nil
}
