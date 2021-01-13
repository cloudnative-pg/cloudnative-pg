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

// scheduledBackupLog is for logging in this package.
var scheduledBackupLog = logf.Log.WithName("scheduledbackup-resource")

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

	v1alphaScheduledBackup := v1alpha1.ScheduledBackup{}
	err := r.ConvertTo(&v1alphaScheduledBackup)
	if err != nil {
		clusterLog.Error(err, "Defaulting webhook for v1")
		return
	}

	v1alphaScheduledBackup.Default()

	err = r.ConvertFrom(&v1alphaScheduledBackup)
	if err != nil {
		clusterLog.Error(err, "Defaulting webhook for v1")
		return
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1-scheduledbackup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,versions=v1,name=vscheduledbackup.kb.io,sideEffects=None

var _ webhook.Validator = &ScheduledBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateCreate() error {
	scheduledBackupLog.Info("validate create", "name", r.Name)

	v1alphaScheduledBackup := v1alpha1.ScheduledBackup{}
	if err := r.ConvertTo(&v1alphaScheduledBackup); err != nil {
		return err
	}

	if err := v1alphaScheduledBackup.ValidateCreate(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateCreate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaScheduledBackup); err != nil {
		return err
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateUpdate(old runtime.Object) error {
	scheduledBackupLog.Info("validate update", "name", r.Name)

	oldV1Alpha1ScheduledBackup := v1alpha1.ScheduledBackup{}
	if err := old.(*Cluster).ConvertTo(&oldV1Alpha1ScheduledBackup); err != nil {
		return err
	}

	v1alphaScheduledBackup := v1alpha1.ScheduledBackup{}
	if err := r.ConvertTo(&v1alphaScheduledBackup); err != nil {
		return err
	}

	if err := v1alphaScheduledBackup.ValidateUpdate(&oldV1Alpha1ScheduledBackup); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateUpdate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaScheduledBackup); err != nil {
		return err
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateDelete() error {
	scheduledBackupLog.Info("validate delete", "name", r.Name)

	v1alphaScheduledBackup := v1alpha1.ScheduledBackup{}
	if err := r.ConvertTo(&v1alphaScheduledBackup); err != nil {
		return err
	}

	if err := v1alphaScheduledBackup.ValidateDelete(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateDelete function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	if err := r.ConvertFrom(&v1alphaScheduledBackup); err != nil {
		return err
	}

	return nil
}
