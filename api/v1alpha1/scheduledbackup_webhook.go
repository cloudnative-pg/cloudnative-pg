/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// scheduledBackupLog is for logging in this package.
var scheduledBackupLog = log.WithName("scheduledbackup-resource").WithValues("version", "v1alpha1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *ScheduledBackup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1alpha1-scheduledbackup,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,verbs=create;update,versions=v1alpha1,name=mscheduledbackupv1alpha1.kb.io,sideEffects=None

var _ webhook.Defaulter = &ScheduledBackup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *ScheduledBackup) Default() {
	scheduledBackupLog.Info("default", "name", r.Name)

	v1ScheduledBackup := v1.ScheduledBackup{}
	err := r.ConvertTo(&v1ScheduledBackup)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}

	v1ScheduledBackup.Default()

	err = r.ConvertFrom(&v1ScheduledBackup)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1alpha1-scheduledbackup,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,versions=v1alpha1,name=vscheduledbackupv1alpha1.kb.io,sideEffects=None

var _ webhook.Validator = &ScheduledBackup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateCreate() error {
	scheduledBackupLog.Info("validate create", "name", r.Name)

	v1ScheduledBackup := v1.ScheduledBackup{}
	if err := r.ConvertTo(&v1ScheduledBackup); err != nil {
		return err
	}

	if err := v1ScheduledBackup.ValidateCreate(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateCreate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1ScheduledBackup)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateUpdate(old runtime.Object) error {
	scheduledBackupLog.Info("validate update", "name", r.Name)

	oldV1ScheduledBackup := v1.ScheduledBackup{}
	if err := old.(*ScheduledBackup).ConvertTo(&oldV1ScheduledBackup); err != nil {
		return err
	}

	v1alphaScheduledBackup := v1.ScheduledBackup{}
	if err := r.ConvertTo(&v1alphaScheduledBackup); err != nil {
		return err
	}

	if err := v1alphaScheduledBackup.ValidateUpdate(&oldV1ScheduledBackup); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateUpdate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1alphaScheduledBackup)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ScheduledBackup) ValidateDelete() error {
	scheduledBackupLog.Info("validate delete", "name", r.Name)

	v1ScheduledBackup := v1.ScheduledBackup{}
	if err := r.ConvertTo(&v1ScheduledBackup); err != nil {
		return err
	}

	if err := v1ScheduledBackup.ValidateDelete(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateDelete function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1ScheduledBackup)
}
