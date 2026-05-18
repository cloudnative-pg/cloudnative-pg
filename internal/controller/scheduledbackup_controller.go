/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/robfig/cron"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// backupParentScheduledBackupIndex is the field indexer key for the parent ScheduledBackup label
	backupParentScheduledBackupIndex = "metadata.labels." + utils.ParentScheduledBackupLabelName

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

	// Check if any backups created by this ScheduledBackup are still running.
	// This provides concurrency control at the ScheduledBackup level.
	childBackups, err := r.GetChildBackups(ctx, scheduledBackup)
	if err != nil {
		contextLogger.Error(err,
			"Cannot extract the list of created backups")
		return ctrl.Result{}, err
	}

	for _, backup := range childBackups {
		if !backup.Status.IsDone() {
			contextLogger.Info(
				"The system is already taking a backup for this ScheduledBackup, retrying in 60 seconds",
				"backupName", backup.GetName(),
				"backupPhase", backup.Status.Phase)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	return r.reconcileScheduledBackup(ctx, &scheduledBackup)
}

// reconcileScheduledBackup is the main reconciliation logic for a scheduled backup
func (r *ScheduledBackupReconciler) reconcileScheduledBackup(
	ctx context.Context,
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
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: scheduledBackup.Namespace,
			Name:      scheduledBackup.Spec.Cluster.Name,
		}, &cluster); err != nil {
			r.Recorder.Eventf(
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
			r.Recorder.Eventf(
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
	if schedule.Next(now).IsZero() {
		// No time satisfying the schedule have been found.
		// We cannot proceed reconciling it.
		r.Recorder.Eventf(
			scheduledBackup,
			"Warning",
			"NoSchedule",
			"No time satisfying the schedule %q have been found", scheduledBackup.Spec.Schedule)
		return ctrl.Result{}, nil
	}

	if scheduledBackup.Status.LastCheckTime == nil && scheduledBackup.IsImmediate() {
		// Operator-restart guard: if a previous reconcile already created the
		// immediate Backup but did not land the status patch, time.Now() on
		// retry differs from the first attempt, so a name-based check would
		// miss the orphan. List by parent + immediate label to catch any
		// existing immediate Backup regardless of its scheduled-time suffix.
		var existingImmediate apiv1.BackupList
		if err := r.List(ctx, &existingImmediate,
			client.InNamespace(scheduledBackup.Namespace),
			client.MatchingLabels{
				utils.ParentScheduledBackupLabelName: scheduledBackup.GetName(),
				utils.ImmediateBackupLabelName:       "true",
			},
		); err != nil {
			return ctrl.Result{}, err
		}
		if len(existingImmediate.Items) > 0 {
			// Upgrade from a pre-fix operator may have created several immediate
			// Backups for the same SB (one per failed status patch). Pick the
			// oldest so LastScheduleTime is stable across reconciles.
			sort.Slice(existingImmediate.Items, func(i, j int) bool {
				return existingImmediate.Items[i].CreationTimestamp.Before(&existingImmediate.Items[j].CreationTimestamp)
			})
			adopted := existingImmediate.Items[0].CreationTimestamp.Time
			return r.advanceScheduledBackupStatus(ctx, scheduledBackup, adopted, now, schedule.Next(now))
		}

		// we populate the status (lastCheckTime...) by following the same rules of the scheduled backup
		r.Recorder.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Scheduled immediate backup now: %v", now)
		return r.createBackup(ctx, scheduledBackup, now, now, schedule, true)
	}

	if scheduledBackup.Status.LastCheckTime == nil {
		origScheduled := scheduledBackup.DeepCopy()
		// This is the first time we check this schedule,
		// let's wait until the first job will be actually
		// scheduled
		scheduledBackup.Status.LastCheckTime = &metav1.Time{
			Time: now,
		}
		err := r.Status().Patch(ctx, scheduledBackup, client.MergeFrom(origScheduled))
		if err != nil {
			return ctrl.Result{}, err
		}

		nextTime := schedule.Next(now)
		contextLogger.Info("Next backup schedule", "next", nextTime)
		r.Recorder.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Scheduled first backup by %v", nextTime)
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// Let's check if we are supposed to start a new backup.
	nextTime := schedule.Next(scheduledBackup.GetStatus().LastCheckTime.Time)
	contextLogger.Info("Next backup schedule", "next", nextTime)

	if now.Before(nextTime) {
		// No need to schedule a new backup, let's wait a bit
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// Observe the apiserver state for this iteration before acting. The Backup
	// name is deterministic (<sb-name>-<compactISO8601(nextTime)>), so we can
	// look it up directly. If it already exists and carries our parent label,
	// a previous reconcile created it but did not land the status patch: adopt
	// that observation and advance the status. Otherwise, create the Backup.
	var existing apiv1.Backup
	switch err := r.Get(ctx, types.NamespacedName{
		Name:      scheduledBackup.BackupName(nextTime),
		Namespace: scheduledBackup.Namespace,
	}, &existing); {
	case apierrs.IsNotFound(err):
		return r.createBackup(ctx, scheduledBackup, nextTime, now, schedule, false)
	case err != nil:
		return ctrl.Result{}, err
	default:
		if existing.Labels[utils.ParentScheduledBackupLabelName] != scheduledBackup.GetName() {
			return r.skipIterationOnNameCollision(ctx, scheduledBackup, &existing, nextTime, now, schedule.Next(now))
		}
		return r.advanceScheduledBackupStatus(ctx, scheduledBackup, nextTime, now, schedule.Next(now))
	}
}

// skipIterationOnNameCollision handles a Backup that occupies this iteration's
// deterministic name but was not created by this ScheduledBackup. Adopting it
// would advance the schedule over a backup we did not run; creating ours would
// loop on AlreadyExists. We skip the colliding slot and resume at the next
// slot from now (cron semantics: missed slots are not retroactively run).
// LastScheduleTime is left untouched so the user can see we did not run this
// iteration. Recovery (deleting the conflicting Backup, retroactively running
// the missed backup) is left to the operator.
func (r *ScheduledBackupReconciler) skipIterationOnNameCollision(
	ctx context.Context,
	scheduledBackup *apiv1.ScheduledBackup,
	existing *apiv1.Backup,
	iteration time.Time,
	now time.Time,
	nextBackupTime time.Time,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Warning("Backup name collision; not adopting",
		"backupName", existing.Name, "iteration", iteration)
	r.Recorder.Eventf(scheduledBackup, "Warning", "BackupAdoptionRefused",
		"Backup %q exists but is not owned by this ScheduledBackup; skipping iteration %s",
		existing.Name, iteration.Format(time.RFC3339))

	origScheduled := scheduledBackup.DeepCopy()
	scheduledBackup.Status.LastCheckTime = &metav1.Time{Time: now}
	scheduledBackup.Status.NextScheduleTime = &metav1.Time{Time: nextBackupTime}
	if err := r.Status().Patch(ctx, scheduledBackup, client.MergeFrom(origScheduled)); err != nil {
		if apierrs.IsConflict(err) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: nextBackupTime.Sub(now)}, nil
}

// createBackup creates a scheduled backup for a backuptime, updating the ScheduledBackup accordingly
func (r *ScheduledBackupReconciler) createBackup(
	ctx context.Context,
	scheduledBackup *apiv1.ScheduledBackup,
	backupTime time.Time,
	now time.Time,
	schedule cron.Schedule,
	immediate bool,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// Deterministic name so retries do not produce duplicates.
	backup := scheduledBackup.CreateBackup(scheduledBackup.BackupName(backupTime))
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
		if err := r.Get(
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
	if err := r.Create(ctx, backup); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// Cache was stale at the Get-first observation in reconcileScheduledBackup
			// (or another reconcile won the race). Requeue so the next pass observes
			// the existing Backup and advances the status from there.
			contextLogger.Debug("Backup already exists, requeuing for re-observation", "error", err)
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		contextLogger.Error(
			err, "Error while creating backup object",
			"backupName", backup.GetName())
		r.Recorder.Event(scheduledBackup, "Warning", "BackupCreation", "Error while creating backup object")
		return ctrl.Result{}, err
	}

	return r.advanceScheduledBackupStatus(ctx, scheduledBackup, backupTime, now, schedule.Next(now))
}

// advanceScheduledBackupStatus records that a Backup for backupTime exists in
// the apiserver and requeues for the next iteration. Both the Get-first
// observation path and the createBackup path funnel through here so the
// invariants stay aligned.
func (r *ScheduledBackupReconciler) advanceScheduledBackupStatus(
	ctx context.Context,
	scheduledBackup *apiv1.ScheduledBackup,
	backupTime time.Time,
	now time.Time,
	nextBackupTime time.Time,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	origScheduled := scheduledBackup.DeepCopy()
	scheduledBackup.Status.LastCheckTime = &metav1.Time{Time: now}
	scheduledBackup.Status.LastScheduleTime = &metav1.Time{Time: backupTime}
	scheduledBackup.Status.NextScheduleTime = &metav1.Time{Time: nextBackupTime}

	if err := r.Status().Patch(ctx, scheduledBackup, client.MergeFrom(origScheduled)); err != nil {
		if apierrs.IsConflict(err) {
			// Stale view of the resource; let the next reconcile re-read and retry.
			contextLogger.Debug("Conflict while updating scheduled backup status", "error", err)
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	contextLogger.Info("Next backup schedule", "next", nextBackupTime)
	r.Recorder.Eventf(scheduledBackup, "Normal", "BackupSchedule", "Next backup scheduled by %v", nextBackupTime)
	return ctrl.Result{RequeueAfter: nextBackupTime.Sub(now)}, nil
}

// GetChildBackups gets all the backups created by a certain ScheduledBackup
// by querying the ParentScheduledBackupLabel using an efficient field indexer.
// This works regardless of the backupOwnerReference configuration.
func (r *ScheduledBackupReconciler) GetChildBackups(
	ctx context.Context,
	scheduledBackup apiv1.ScheduledBackup,
) ([]apiv1.Backup, error) {
	var childBackups apiv1.BackupList

	if err := r.List(ctx, &childBackups,
		client.InNamespace(scheduledBackup.Namespace),
		client.MatchingFields{backupParentScheduledBackupIndex: scheduledBackup.GetName()},
	); err != nil {
		return nil, fmt.Errorf("unable to list child backups: %w", err)
	}

	return childBackups.Items, nil
}

// SetupWithManager install this controller in the controller manager
func (r *ScheduledBackupReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	maxConcurrentReconciles int,
) error {
	// Create a field indexer on the parent ScheduledBackup label to efficiently
	// find all backups created by a ScheduledBackup, regardless of the
	// backupOwnerReference configuration.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&apiv1.Backup{},
		backupParentScheduledBackupIndex, func(rawObj client.Object) []string {
			backup := rawObj.(*apiv1.Backup)
			if backup.Labels == nil {
				return nil
			}

			if parent, ok := backup.Labels[ParentScheduledBackupLabelName]; ok {
				return []string{parent}
			}
			return nil
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(&apiv1.ScheduledBackup{}).
		Named("scheduled-backup").
		Complete(r)
}
