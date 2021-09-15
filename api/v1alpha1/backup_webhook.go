/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// backupLog is for logging in this package.
var backupLog = log.WithName("backup-resource").WithValues("version", "v1alpha1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1alpha1-backup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=create;update,versions=v1alpha1,name=mbackupv1alpha1.kb.io,sideEffects=None

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name)

	v1Backup := v1.Backup{}
	err := r.ConvertTo(&v1Backup)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}

	v1Backup.Default()

	err = r.ConvertFrom(&v1Backup)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1alpha1-backup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,versions=v1alpha1,name=vbackupv1alpha1.kb.io,sideEffects=None

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() error {
	backupLog.Info("validate create", "name", r.Name)

	v1Backup := v1.Backup{}
	if err := r.ConvertTo(&v1Backup); err != nil {
		return err
	}

	if err := v1Backup.ValidateCreate(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateCreate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Backup)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(old runtime.Object) error {
	backupLog.Info("validate update", "name", r.Name)

	oldV1Backup := v1.Backup{}
	if err := old.(*Backup).ConvertTo(&oldV1Backup); err != nil {
		return err
	}

	v1Backup := v1.Backup{}
	if err := r.ConvertTo(&v1Backup); err != nil {
		return err
	}

	if err := v1Backup.ValidateUpdate(&oldV1Backup); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateUpdate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Backup)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() error {
	backupLog.Info("validate delete", "name", r.Name)

	v1Backup := v1.Backup{}
	if err := r.ConvertTo(&v1Backup); err != nil {
		return err
	}

	if err := v1Backup.ValidateDelete(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateDelete function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Backup)
}
