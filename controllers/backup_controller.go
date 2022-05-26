/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// backupPhase indicates the path inside the Backup kind
// where the phase can be located
const backupPhase = ".status.phase"

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get
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

	contextLogger.Debug("Found cluster for backup", "cluster", clusterName)

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
	contextLogger.Debug("Found pod for backup", "pod", pod.Name)

	if !utils.IsPodReady(pod) {
		contextLogger.Info("Not ready backup target, will retry in 30 seconds", "target", pod.Name)
		backup.Status.Phase = apiv1.BackupPhasePending
		r.Recorder.Eventf(&backup, "Warning", "BackupPending", "Backup target pod not ready: %s",
			cluster.Status.TargetPrimary)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.Status().Update(ctx, &backup)
	}

	if backup.Status.Phase != "" && backup.Status.InstanceID != nil {
		// Detect the pod where a backup will be executed
		var pod corev1.Pod
		err = r.Get(ctx, client.ObjectKey{
			Namespace: backup.Namespace,
			Name:      backup.Status.InstanceID.PodName,
		}, &pod)
		// we found the pod
		if err == nil &&
			// the pod is actually the target primary,
			// we don't care whether it's the current one as running the backup on the new primary would
			// still be the correct thing to do
			backup.Status.InstanceID.PodName == cluster.Status.TargetPrimary &&
			// the pod was not restarted since when we started the backup
			backup.Status.InstanceID.ContainerID == pod.Status.ContainerStatuses[0].ContainerID &&
			// the pod is active
			utils.IsPodActive(pod) {
			contextLogger.Info("Backup is already running on",
				"cluster", cluster.Name,
				"pod", pod.Name,
				"started at", backup.Status.StartedAt)

			// Nothing to do here
			return ctrl.Result{}, nil
		}
		// We need to restart the backup as the previously selected instance doesn't look healthy
		r.Recorder.Eventf(&backup, "Normal", "ReStarting",
			"Restarted backup for cluster %v on instance %v", clusterName, pod.Name)
	} else {
		// We need to start a backup
		r.Recorder.Eventf(&backup, "Normal", "Starting", "Starting backup for cluster %v", clusterName)
	}

	contextLogger.Info("Starting backup",
		"cluster", cluster.Name,
		"pod", pod.Name)

	// This backup has been started
	err = StartBackup(ctx, r.Client, &backup, pod, &cluster)
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
	cluster *apiv1.Cluster,
) error {
	// This backup has been started
	status := backup.GetStatus()
	status.Phase = apiv1.BackupPhaseStarted
	status.InstanceID = &apiv1.InstanceID{PodName: pod.Name, ContainerID: pod.Status.ContainerStatuses[0].ContainerID}
	if err := postgres.UpdateBackupStatusAndRetry(ctx, client, backup); err != nil {
		status.SetAsFailed(fmt.Errorf("can't update backup: %w", err))
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
			backup.GetName(),
		)
		return err
	})

	if err != nil {
		log.FromContext(ctx).Error(err, "executing backup", "stdout", stdout, "stderr", stderr)
		status.SetAsFailed(fmt.Errorf("can't execute backup: %w", err))
		status.CommandError = stderr
		status.CommandError = stdout

		// Update backup status in cluster conditions
		condition := metav1.Condition{
			Type:    string(apiv1.ConditionBackup),
			Status:  metav1.ConditionFalse,
			Reason:  "LastBackupFailed",
			Message: err.Error(),
		}
		if errCond := manager.UpdateCondition(ctx, client, cluster, &condition); errCond != nil {
			log.FromContext(ctx).Error(errCond, "Error while updating conditions")
		}
		return postgres.UpdateBackupStatusAndRetry(ctx, client, backup)
	}

	return nil
}

// SetupWithManager sets up this controller given a controller manager
func (r *BackupReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Backup{},
		backupPhase, func(rawObj client.Object) []string {
			return []string{string(rawObj.(*apiv1.Backup).Status.Phase)}
		}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Backup{}).
		Watches(&source.Kind{Type: &apiv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(r.mapClustersToBackup(ctx)),
			builder.WithPredicates(clustersWithBackupPredicate),
		).
		Complete(r)
}

func (r *BackupReconciler) mapClustersToBackup(ctx context.Context) handler.MapFunc {
	return func(obj client.Object) []reconcile.Request {
		cluster, ok := obj.(*apiv1.Cluster)
		if !ok {
			return nil
		}
		var backups apiv1.BackupList
		err := r.Client.List(ctx, &backups,
			client.MatchingFields{
				backupPhase: apiv1.BackupPhaseRunning,
			},
			client.InNamespace(cluster.GetNamespace()))
		if err != nil {
			log.FromContext(ctx).Error(err, "while getting running backups for cluster", "cluster", cluster.GetName())
		}
		var requests []reconcile.Request
		for _, backup := range backups.Items {
			if backup.Spec.Cluster.Name == cluster.Name {
				continue
			}
			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      backup.Name,
						Namespace: backup.Namespace,
					},
				},
			)
		}
		return requests
	}
}

var clustersWithBackupPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	GenericFunc: func(e event.GenericEvent) bool {
		cluster, ok := e.Object.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		cluster, ok := e.ObjectNew.(*apiv1.Cluster)
		if !ok {
			return false
		}
		return cluster.Spec.Backup != nil
	},
}
