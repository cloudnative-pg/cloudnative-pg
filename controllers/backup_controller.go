/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	postgresqlv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=clusters,verbs=get
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get

// Reconcile is the main reconciliation loop
func (r *BackupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("backup", req.NamespacedName)

	var backup postgresqlv1alpha1.Backup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		// This also happens when you delete a Backup resource in k8s.
		// If that's the case, we have nothing to do
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if len(backup.Status.Phase) != 0 {
		// Nothing to do here
		return ctrl.Result{}, nil
	}

	// We need to start a backup
	clusterName := backup.Spec.Cluster.Name
	var cluster postgresqlv1alpha1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      clusterName,
	}, &cluster); err != nil {
		if apierrs.IsNotFound(err) {
			backup.Status.SetAsFailed("Unknown cluster", "", err)
			return ctrl.Result{}, r.Status().Update(ctx, &backup)
		}
		return ctrl.Result{}, err
	}

	// Detect the pod where a backup will be executed
	var pod corev1.Pod
	err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      cluster.Status.TargetPrimary,
	}, &pod)
	if err != nil {
		backup.Status.SetAsFailed("Unknown pod", "", err)
		return ctrl.Result{}, r.Status().Update(ctx, &backup)
	}

	log.Info("Starting backup",
		"cluster", cluster.Name,
		"pod", pod.Name)

	// This backup has been started
	err = StartBackup(ctx, r, &backup, pod)
	return ctrl.Result{}, err
}

// StartBackup request a backup in a Pod and marks the backup started
// or failed if needed
func StartBackup(
	ctx context.Context,
	client client.StatusClient,
	backup postgresqlv1alpha1.BackupCommon,
	pod corev1.Pod,
) error {
	// This backup has been started
	backup.GetStatus().Phase = postgresqlv1alpha1.BackupPhaseStarted
	if err := utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject()); err != nil {
		backup.GetStatus().SetAsFailed("Can't update backup", "", err)
		return err
	}

	stdout, stderr, err := utils.ExecCommand(
		ctx,
		pod,
		specs.PostgresContainerName,
		nil,
		"/controller/manager",
		"backup",
		backup.GetName())
	if err != nil {
		backup.GetStatus().SetAsFailed(stdout, stderr, err)
		return utils.UpdateStatusAndRetry(ctx, client, backup.GetKubernetesObject())
	}

	return nil
}

// SetupWithManager sets up this controller given a controller manager
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresqlv1alpha1.Backup{}).
		Complete(r)
}
