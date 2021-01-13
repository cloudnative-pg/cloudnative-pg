/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
)

// backupLog is for logging in this package.
var backupLog = logf.Log.WithName("backup-resource")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1-backup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=create;update,versions=v1,name=mbackup.kb.io,sideEffects=None

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Backup) Default() {
	backupLog.Info("default", "name", r.Name)

	v1alphaBackup := v1alpha1.Backup{}
	err := r.ConvertTo(&v1alphaBackup)
	if err != nil {
		clusterLog.Error(err, "Defaulting webhook for v1")
		return
	}

	v1alphaBackup.Default()

	err = r.ConvertFrom(&v1alphaBackup)
	if err != nil {
		clusterLog.Error(err, "Defaulting webhook for v1")
		return
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-backup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=backups,versions=v1,name=vbackup.kb.io,sideEffects=None

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateCreate() error {
	backupLog.Info("validate create", "name", r.Name)

	v1alphaBackup := v1alpha1.Backup{}
	if err := r.ConvertTo(&v1alphaBackup); err != nil {
		return err
	}

	if err := v1alphaBackup.ValidateCreate(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateCreate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaBackup); err != nil {
		return err
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateUpdate(old runtime.Object) error {
	backupLog.Info("validate update", "name", r.Name)

	oldV1Alpha1Backup := v1alpha1.Backup{}
	if err := old.(*Cluster).ConvertTo(&oldV1Alpha1Backup); err != nil {
		return err
	}

	v1alphaBackup := v1alpha1.Backup{}
	if err := r.ConvertTo(&v1alphaBackup); err != nil {
		return err
	}

	if err := v1alphaBackup.ValidateUpdate(&oldV1Alpha1Backup); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateUpdate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaBackup); err != nil {
		return err
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Backup) ValidateDelete() error {
	backupLog.Info("validate delete", "name", r.Name)

	v1alphaBackup := v1alpha1.Backup{}
	if err := r.ConvertTo(&v1alphaBackup); err != nil {
		return err
	}

	if err := v1alphaBackup.ValidateDelete(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateDelete function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaBackup); err != nil {
		return err
	}

	return nil
}
