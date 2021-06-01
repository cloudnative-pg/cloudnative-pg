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

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// clusterLog is for logging in this package.
var clusterLog = logf.Log.WithName("cluster-resource").WithValues("version", "v1alpha1")

// SetupWebhookWithManager setup the webhook inside the controller manager
func (r *Cluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},path=/mutate-postgresql-k8s-enterprisedb-io-v1alpha1-cluster,mutating=true,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=create;update,versions=v1alpha1,name=mclusterv1alpha1.kb.io,sideEffects=None

var _ webhook.Defaulter = &Cluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Cluster) Default() {
	clusterLog.Info("default", "name", r.Name)

	v1Cluster := v1.Cluster{}
	err := r.ConvertTo(&v1Cluster)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}

	v1Cluster.Default()

	err = r.ConvertFrom(&v1Cluster)
	if err != nil {
		clusterLog.Error(err, "Invoking defaulting webhook from v1")
		return
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:webhookVersions={v1beta1},admissionReviewVersions={v1beta1},verbs=create;update,path=/validate-postgresql-k8s-enterprisedb-io-v1alpha1-cluster,mutating=false,failurePolicy=fail,groups=postgresql.k8s.enterprisedb.io,resources=clusters,versions=v1alpha1,name=vclusterv1alpha1.kb.io,sideEffects=None

var _ webhook.Validator = &Cluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateCreate() error {
	clusterLog.Info("validate create", "name", r.Name)

	v1Cluster := v1.Cluster{}
	if err := r.ConvertTo(&v1Cluster); err != nil {
		return err
	}

	if err := v1Cluster.ValidateCreate(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateCreate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Cluster)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateUpdate(old runtime.Object) error {
	clusterLog.Info("validate update", "name", r.Name)

	oldV1Cluster := v1.Cluster{}
	if err := old.(*Cluster).ConvertTo(&oldV1Cluster); err != nil {
		return err
	}

	v1Cluster := v1.Cluster{}
	if err := r.ConvertTo(&v1Cluster); err != nil {
		return err
	}

	if err := v1Cluster.ValidateUpdate(&oldV1Cluster); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateUpdate function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Cluster)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Cluster) ValidateDelete() error {
	clusterLog.Info("validate delete", "name", r.Name)

	v1Cluster := v1.Cluster{}
	if err := r.ConvertTo(&v1Cluster); err != nil {
		return err
	}

	if err := v1Cluster.ValidateDelete(); err != nil {
		return err
	}

	// TODO: Remove the ConvertFrom call
	// This code is not useful because the ValidateDelete function
	// is not supposed to change the object. However this call helps to catch
	// issues in the conversion code
	return r.ConvertFrom(&v1Cluster)
}
