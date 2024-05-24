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

package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/robfig/cron"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	backupOwnerKey = ".metadata.controller"

	// ImmediateBackupLabelName label is applied to backups to tell if a backup
	// is immediate or not
	ImmediateBackupLabelName = utils.ImmediateBackupLabelName

	// ParentScheduledBackupLabelName label is applied to backups to easily tell the scheduled backup
	// it was created from.
	ParentScheduledBackupLabelName = utils.ParentScheduledBackupLabelName
)

// ScheduledBackupReconciler reconciles a ScheduledBackup object
type ScheduledBackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the main reconciler logic
func (r *ScheduledBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug(fmt.Sprintf("reconciling object %#q", req.NamespacedName))

	defer func() {
		contextLogger.Debug(fmt.Sprintf("object %#q has been reconciled", req.NamespacedName))
	}()

	var scheduledBackup apiv1.ScheduledBackup
	if err := r.Get(ctx, req.NamespacedName, &scheduledBackup); err != nil {
		// This also happens when you delete a Backup resource in k8s.
		// If that's the case, we have nothing to do
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// This check is still needed for when the scheduled backup resource creation is forced through the webhook
	if scheduledBackup.Spec.Method == apiv1.BackupMethodVolumeSnapshot && !utils.HaveVolumeSnapshot() {
		contextLogger.Error(
			errors.New("cannot execute due to missing VolumeSnapshot CRD"),
			"While checking for VolumeSnapshot CRD",
		)
		return ctrl.Result{}, nil
	}

	if scheduledBackup.IsSuspended() {
		contextLogger.Info("Skipping as backup is suspended")
		return ctrl.Result{}, nil
	}

	// We are supposed to start a new backup. Let's extract
	// the list of backups we have already taken to see if anything
	// is running now
	childBackups, err := r.GetChildBackups(ctx, scheduledBackup)
	if err != nil {
		contextLogger.Error(err,
			"Cannot extract the list of created backups")
		return ctrl.Result{}, err
	}

	// We are supposed to start a new backup. Let's extract
	// the list of backups we have already taken to see if anything
	// is running now
	for _, backup := range childBackups {
		if !backup.Status.IsDone() {
			contextLogger.Info(
				"The system is already taking a scheduledBackup, retrying in 60 seconds",
				"backupName", backup.GetName(),
				"backupPhase", backup.Status.Phase)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	return ReconcileScheduledBackup(ctx, r.Recorder, r.Client, &scheduledBackup)
}

// ReconcileScheduledBackup is the main reconciliation logic for a scheduled backup
func ReconcileScheduledBackup(
	ctx context.Context,
	event record.EventRecorder,
	cli client.Client,
	scheduledBackup *apiv1.ScheduledBackup,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// Let's check
	schedule, err := cron.Parse(scheduledBackup.GetSchedule())
	if err != nil {
		contextLogger.Info("Detected an invalid cron schedule",
			"schedule", scheduledBackup.GetSchedule())
		return ctrl.Result{}, err
	}

	// Immediate volume snapshot backups can be scheduled only when the cluster
	// is ready as taking a cold backup meanwhile is being created may stop the
	// cluster creation because the primary instance could be fenced.
	isVolumeSnapshot := scheduledBackup.Spec.Method == apiv1.BackupMethodVolumeSnapshot
	if isVolumeSnapshot && scheduledBackup.Status.LastCheckTime == nil && scheduledBackup.IsImmediate() {
		var cluster apiv1.Cluster
		if err := cli.Get(ctx, client.ObjectKey{
			Namespace: scheduledBackup.Namespace,
			Name:      scheduledBackup.Spec.Cluster.Name,
		}, &cluster); err != nil {
			event.Eventf(
				scheduledBackup,
				"Normal",
				"InvalidCluster",
				"Cannot get cluster %v, %v",
				scheduledBackup.Spec.Cluster.Name,
				err.Error(),
			)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if cluster.Status.Phase != apiv1.PhaseHealthy {
			event.Eventf(
				scheduledBackup,
				"Warning",
				"ClusterNotHealthy",
				"Waiting for cluster to be healthy, was \"%v\"",
				cluster.Status.Phase,
			)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	now := time.Now()
	origScheduled := scheduledBackup.DeepCopy()

	if scheduledBackup.Status.LastCheckTime == nil {
		// This is the first time we check this schedule,
		// let's wait until the first job will be actually
		// scheduled
		scheduledBackup.Status.LastCheckTime = &metav1.Time{
			Time: now,
		}
		err := cli.Status().Patch(ctx, scheduledBackup, client.MergeFrom(origScheduled))
		if err != nil {
			return ctrl.Result{}, err
		}

		if scheduledBackup.IsImmediate() {
			event.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Scheduled immediate backup now: %v", now)
			return createBackup(ctx, event, cli, scheduledBackup, now, now, schedule, true)
		}

		nextTime := schedule.Next(now)
		contextLogger.Info("Next backup schedule", "next", nextTime)
		event.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Scheduled first backup by %v", nextTime)
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// Let's check if we are supposed to start a new backup.
	nextTime := schedule.Next(scheduledBackup.GetStatus().LastCheckTime.Time)
	contextLogger.Info("Next backup schedule", "next", nextTime)

	if now.Before(nextTime) {
		// No need to schedule a new backup, let's wait a bit
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	return createBackup(ctx, event, cli, scheduledBackup, nextTime, now, schedule, false)
}

// createBackup creates a scheduled backup for a backuptime, updating the ScheduledBackup accordingly
func createBackup(
	ctx context.Context,
	event record.EventRecorder,
	cli client.Client,
	scheduledBackup *apiv1.ScheduledBackup,
	backupTime time.Time,
	now time.Time,
	schedule cron.Schedule,
	immediate bool,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	origScheduled := scheduledBackup.DeepCopy()

	// So we have no backup running, let's create a backup.
	// Let's have deterministic names to avoid creating the job two
	// times
	name := fmt.Sprintf("%s-%s", scheduledBackup.GetName(), utils.ToCompactISO8601(backupTime))
	backup := scheduledBackup.CreateBackup(name)
	metadata := &backup.ObjectMeta
	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string)
	}
	metadata.Labels[utils.ClusterLabelName] = scheduledBackup.Spec.Cluster.Name
	metadata.Labels[utils.ImmediateBackupLabelName] = strconv.FormatBool(immediate)
	metadata.Labels[utils.ParentScheduledBackupLabelName] = scheduledBackup.GetName()

	switch scheduledBackup.Spec.BackupOwnerReference {
	case "cluster":
		var cluster apiv1.Cluster
		if err := cli.Get(
			ctx,
			types.NamespacedName{Name: scheduledBackup.Spec.Cluster.Name, Namespace: scheduledBackup.Namespace},
			&cluster,
		); err != nil {
			return ctrl.Result{}, err
		}
		cluster.SetInheritedDataAndOwnership(&backup.ObjectMeta)
	case "self":
		utils.SetAsOwnedBy(&backup.ObjectMeta, scheduledBackup.ObjectMeta, scheduledBackup.TypeMeta)
	default:
		// the default behaviour is `none`, means no owner
		break
	}

	contextLogger.Info("Creating backup", "backupName", backup.Name)
	if err := cli.Create(ctx, backup); err != nil {
		if apierrs.IsConflict(err) {
			// Retry later, the cache is stale
			contextLogger.Debug("Conflict while creating backup", "error", err)
			return ctrl.Result{}, nil
		}

		contextLogger.Error(
			err, "Error while creating backup object",
			"backupName", backup.GetName())
		event.Event(scheduledBackup, "Warning", "BackupCreation", "Error while creating backup object")
		return ctrl.Result{}, err
	}

	// Ok, now update the latest check to now
	scheduledBackup.Status.LastCheckTime = &metav1.Time{
		Time: now,
	}
	scheduledBackup.Status.LastScheduleTime = &metav1.Time{
		Time: backupTime,
	}
	nextBackupTime := schedule.Next(now)
	scheduledBackup.Status.NextScheduleTime = &metav1.Time{
		Time: nextBackupTime,
	}

	if err := cli.Status().Patch(ctx, scheduledBackup, client.MergeFrom(origScheduled)); err != nil {
		if apierrs.IsConflict(err) {
			// Retry later, the cache is stale
			contextLogger.Debug("Conflict while updating scheduled backup", "error", err)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	contextLogger.Info("Next backup schedule", "next", backupTime)
	event.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Next backup scheduled by %v", nextBackupTime)
	return ctrl.Result{RequeueAfter: nextBackupTime.Sub(now)}, nil
}

// GetChildBackups gets all the backups scheduled by a certain scheduler
func (r *ScheduledBackupReconciler) GetChildBackups(
	ctx context.Context,
	scheduledBackup apiv1.ScheduledBackup,
) ([]apiv1.Backup, error) {
	var childBackups apiv1.BackupList

	if err := r.List(ctx, &childBackups,
		client.InNamespace(scheduledBackup.Namespace),
		client.MatchingFields{backupOwnerKey: scheduledBackup.Name},
	); err != nil {
		return nil, fmt.Errorf("unable to list child pods resource: %w", err)
	}

	return childBackups.Items, nil
}

// SetupWithManager install this controller in the controller manager
func (r *ScheduledBackupReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Create a new indexed field on backups. This field will be used to easily
	// find all the backups created by this controller
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Backup{},
		backupOwnerKey, func(rawObj client.Object) []string {
			pod := rawObj.(*apiv1.Backup)
			owner := metav1.GetControllerOf(pod)
			if owner == nil {
				return nil
			}

			if owner.Kind != apiv1.BackupKind {
				return nil
			}

			if owner.APIVersion != apiGVString {
				return nil
			}

			return []string{owner.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.ScheduledBackup{}).
		Complete(r)
}
