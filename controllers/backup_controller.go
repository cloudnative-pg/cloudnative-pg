/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=clusters,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;delete;patch;create;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get

// Reconcile is the main reconciliation loop
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug(fmt.Sprintf("reconciling object %#q", req.NamespacedName))

	defer func() {
		contextLogger.Debug(fmt.Sprintf("object %#q has been reconciled", req.NamespacedName))
	}()

	var backup apiv1.Backup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		// This also happens when you delete a Backup resource in k8s.
		// If that's the case, we have nothing to do
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if len(backup.Status.Phase) != 0 && backup.Status.Phase != apiv1.BackupPhasePending {
		// Nothing to do here
		return ctrl.Result{}, nil
	}

	// We need to start a backup
	clusterName := backup.Spec.Cluster.Name
	var cluster apiv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      clusterName,
	}, &cluster); err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(&backup, "Warning", "FindingCluster",
				"Unknown cluster %v, will retry in 30 seconds", clusterName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		backup.Status.SetAsFailed(fmt.Errorf("while getting cluster %s: %w", clusterName, err))
		r.Recorder.Eventf(&backup, "Warning", "FindingCluster",
			"Error getting cluster %v, will not retry: %s", clusterName, err.Error())
		return ctrl.Result{}, err
	}

	r.Recorder.Eventf(&backup, "Normal", "FindingCluster", "Found cluster %v", clusterName)

	// Detect the pod where a backup will be executed
	var pod corev1.Pod
	err := r.Get(ctx, client.ObjectKey{
		Namespace: backup.Namespace,
		Name:      cluster.Status.TargetPrimary,
	}, &pod)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(&backup, "Warning", "FindingPod",
				"Couldn't find target pod %s, will retry in 30 seconds", cluster.Status.TargetPrimary)
			contextLogger.Info("Couldn't find target pod, will retry in 30 seconds", "target",
				cluster.Status.TargetPrimary)
			backup.Status.Phase = apiv1.BackupPhasePending
			return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Update(ctx, &backup)
		}
		backup.Status.SetAsFailed(fmt.Errorf("while getting pod: %w", err))
		r.Recorder.Eventf(&backup, "Warning", "FindingPod", "Error getting target pod: %s",
			cluster.Status.TargetPrimary)
		return ctrl.Result{}, r.Status().Update(ctx, &backup)
	}

	if !utils.IsPodReady(pod) {
		contextLogger.Info("Not ready backup target, will retry in 30 seconds", "target", pod.Name)
		backup.Status.Phase = apiv1.BackupPhasePending
		r.Recorder.Eventf(&backup, "Warning", "BackupPending", "Backup target pod not ready: %s",
			cluster.Status.TargetPrimary)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Update(ctx, &backup)
	}

	r.Recorder.Eventf(&backup, "Normal", "Starting", "Started backup for cluster %v",
		clusterName)
	contextLogger.Info("Starting backup",
		"cluster", cluster.Name,
		"pod", pod.Name)

	// This backup has been started
	err = StartBackup(ctx, r.Client, &backup, pod)
	if err != nil {
		r.Recorder.Eventf(&backup, "Warning", "Error", "Backup exit with error %v", err)
	}

	return ctrl.Result{}, err
}

// StartBackup request a backup in a Pod and marks the backup started
// or failed if needed
func StartBackup(
	ctx context.Context,
	client client.Client,
	backup *apiv1.Backup,
	pod corev1.Pod,
) error {
	// This backup has been started
	backup.GetStatus().Phase = apiv1.BackupPhaseStarted
	if err := postgres.UpdateBackupStatusAndRetry(ctx, client, backup); err != nil {
		backup.GetStatus().SetAsFailed(fmt.Errorf("can't update backup: %w", err))
		return err
	}
	config := ctrl.GetConfigOrDie()
	clientInterface := kubernetes.NewForConfigOrDie(config)

	var err error
	var stdout, stderr string
	err = retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
		stdout, stderr, err = utils.ExecCommand(
			ctx,
			clientInterface,
			config,
			pod,
			specs.PostgresContainerName,
			nil,
			"/controller/manager",
			"backup",
			backup.GetName())
		return err
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "executing backup", "stdout", stdout, "stderr", stderr)
		status := backup.GetStatus()
		status.SetAsFailed(fmt.Errorf("can't execute backup: %w", err))
		status.CommandError = stderr
		status.CommandError = stdout
		return postgres.UpdateBackupStatusAndRetry(ctx, client, backup)
	}

	return nil
}

// SetupWithManager sets up this controller given a controller manager
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Backup{}).
		Complete(r)
}
