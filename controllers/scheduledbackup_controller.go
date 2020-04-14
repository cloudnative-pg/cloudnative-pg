/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/robfig/cron"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	postgresqlv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

const (
	backupOwnerKey = ".metadata.controller"
)

// ScheduledBackupReconciler reconciles a ScheduledBackup object
type ScheduledBackupReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=scheduledbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.2ndq.io,resources=backups,verbs=get;list;create

// Reconcile is the main reconciler logic
func (r *ScheduledBackupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("scheduledbackup", req.NamespacedName)

	var scheduledBackup postgresqlv1alpha1.ScheduledBackup
	if err := r.Get(ctx, req.NamespacedName, &scheduledBackup); err != nil {
		// This also happens when you delete a Backup resource in k8s.
		// If that's the case, we have nothing to do
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Let's check
	schedule, err := cron.Parse(scheduledBackup.Spec.Schedule)
	if err != nil {
		r.Log.Info("Detected an invalid cron schedule",
			"schedule", scheduledBackup.Spec.Schedule,
			"namespace", scheduledBackup.Namespace,
			"name", scheduledBackup.Name)
		return ctrl.Result{}, err
	}

	now := time.Now()

	if scheduledBackup.Status.LastCheckTime == nil {
		// This is the first time we check this schedule,
		// let's wait until the first job will be actually
		// scheduled
		scheduledBackup.Status.LastCheckTime = &metav1.Time{
			Time: now,
		}
		err := r.Status().Update(ctx, &scheduledBackup)
		if err != nil {
			if apierrs.IsConflict(err) {
				// Retry later, the cache is stale
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		nextTime := schedule.Next(scheduledBackup.Status.LastCheckTime.Time)
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// This scheduledBackup has already been scheduled, let's check if
	// we are supposed to start a new backup.
	nextTime := schedule.Next(scheduledBackup.Status.LastCheckTime.Time)
	if now.Before(nextTime) {
		// No need to schedule a new backup, let's wait a bit
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// We are supposed to start a new backup. Let's extract
	// the list of backups we already taken to see if anything
	// is running now
	childBackups, err := r.GetChildBackups(ctx, scheduledBackup)
	if err != nil {
		r.Log.Error(err,
			"Cannot extract the list of created backups",
			"namespace", scheduledBackup.Namespace,
			"name", scheduledBackup.Name)
		return ctrl.Result{}, err
	}

	for _, backup := range childBackups {
		if backup.IsInProgress() {
			r.Log.Info(
				"The system is already taking a scheduledBackup, skipping this one",
				"namespace", scheduledBackup.Namespace,
				"name", scheduledBackup.Name,
				"backupName", backup.Name,
				"backupPhase", backup.Status.Phase)
			return ctrl.Result{}, nil
		}
	}

	// So we have no backup running, let's create a backup.
	// Let's have deterministic names to avoid creating the job two
	// times
	name := fmt.Sprintf("%s-%d", scheduledBackup.Name, nextTime.Unix())
	backup := postgresqlv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scheduledBackup.Namespace,
		},
		Spec: postgresqlv1alpha1.BackupSpec{
			Cluster: scheduledBackup.Spec.Cluster,
		},
	}
	utils.SetAsOwnedBy(&backup.ObjectMeta, scheduledBackup.ObjectMeta, scheduledBackup.TypeMeta)

	r.Log.Info("Creating backup",
		"name", scheduledBackup.Name,
		"namespace", scheduledBackup.Namespace,
		"backupName", backup.Name)
	if err = r.Create(ctx, &backup); err != nil {
		if !apierrs.IsConflict(err) {
			r.Log.Error(err, "Error while creating backup object",
				"name", scheduledBackup.Name,
				"namespace", scheduledBackup.Namespace,
				"backupName", backup.Name)
			return ctrl.Result{}, err
		}
	}

	// Ok, now update the latest check to now
	scheduledBackup.Status.LastCheckTime = &metav1.Time{
		Time: now,
	}
	if err = r.Status().Update(ctx, &scheduledBackup); err != nil {
		if apierrs.IsConflict(err) {
			// Retry later, the cache is stale
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// GetChildBackups gets all the backups scheduled by a certain scheduler
func (r *ScheduledBackupReconciler) GetChildBackups(
	ctx context.Context,
	scheduledBackup postgresqlv1alpha1.ScheduledBackup,
) ([]postgresqlv1alpha1.Backup, error) {
	var childBackups postgresqlv1alpha1.BackupList

	if err := r.List(ctx, &childBackups,
		client.InNamespace(scheduledBackup.Namespace),
		client.MatchingFields{backupOwnerKey: scheduledBackup.Name},
	); err != nil {
		r.Log.Error(err, "Unable to list child pods resource",
			"namespace", scheduledBackup.Namespace,
			"name", scheduledBackup.Name)
		return nil, err
	}

	return childBackups.Items, nil
}

// SetupWithManager install this controller in the controller manager
func (r *ScheduledBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a new indexed field on backups. This field will be used to easily
	// find all the backups created by this controller
	if err := mgr.GetFieldIndexer().IndexField(
		&postgresqlv1alpha1.Backup{},
		backupOwnerKey, func(rawObj runtime.Object) []string {
			pod := rawObj.(*postgresqlv1alpha1.Backup)
			owner := metav1.GetControllerOf(pod)
			if owner == nil {
				return nil
			}

			if owner.APIVersion != apiGVString || owner.Kind != "ScheduledBackup" {
				return nil
			}

			return []string{owner.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresqlv1alpha1.ScheduledBackup{}).
		Complete(r)
}
