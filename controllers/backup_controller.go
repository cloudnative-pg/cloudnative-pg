/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	postgresqlv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
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
	_ = r.Log.WithValues("backup", req.NamespacedName)

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
			backup.SetAsFailed("Unknown cluster", "", err)
			return ctrl.Result{}, r.UpdateAndRetry(ctx, &backup)
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
		backup.SetAsFailed("Unknown pod", "", err)
		return ctrl.Result{}, r.UpdateAndRetry(ctx, &backup)
	}

	r.Log.Info("Starting backup",
		"name", backup.Name,
		"namespace", backup.Namespace,
		"cluster", cluster.Name,
		"pod", pod.Name)

	// This backup has been started
	backup.Status.Phase = postgresqlv1alpha1.BackupPhaseStarted
	err = r.UpdateAndRetry(ctx, &backup)
	if err != nil {
		backup.SetAsFailed("Unknown pod", "", err)
		return ctrl.Result{}, r.UpdateAndRetry(ctx, &backup)
	}

	stdout, stderr, err := utils.ExecCommand(
		ctx,
		pod,
		specs.PostgresContainerName,
		nil,
		"/controller/manager",
		"backup",
		backup.Name)
	if err != nil {
		backup.SetAsFailed(stdout, stderr, err)
		return ctrl.Result{}, r.UpdateAndRetry(ctx, &backup)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up this controller given a controller manager
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresqlv1alpha1.Backup{}).
		Complete(r)
}

// UpdateAndRetry update a certain backup in the k8s database
// retrying when conflicts are detected
func (r *BackupReconciler) UpdateAndRetry(
	ctx context.Context,
	backup *postgresqlv1alpha1.Backup,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.Update(ctx, backup)
	})
}

// MarkAsStarted marks a certain backup as invalid
func (r *BackupReconciler) MarkAsStarted(ctx context.Context, backup *postgresqlv1alpha1.Backup) error {
	backup.Status.Phase = postgresqlv1alpha1.BackupPhaseStarted
	backup.Status.StartedAt = &metav1.Time{
		Time: time.Now(),
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.Update(ctx, backup)
	})
}
