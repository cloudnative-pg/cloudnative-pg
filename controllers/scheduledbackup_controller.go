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

	postgresqlv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
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

// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=scheduledbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.k8s.enterprisedb.io,resources=backups,verbs=get;list;create

// Reconcile is the main reconciler logic
func (r *ScheduledBackupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("scheduledbackup", req.NamespacedName)

	var scheduledBackup postgresqlv1alpha1.ScheduledBackup
	if err := r.Get(ctx, req.NamespacedName, &scheduledBackup); err != nil {
		// This also happens when you delete a Backup resource in k8s.
		// If that's the case, we have nothing to do
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// We are supposed to start a new backup. Let's extract
	// the list of backups we already taken to see if anything
	// is running now
	childBackups, err := r.GetChildBackups(ctx, scheduledBackup)
	if err != nil {
		log.Error(err,
			"Cannot extract the list of created backups")
		return ctrl.Result{}, err
	}

	// We are supposed to start a new backup. Let's extract
	// the list of backups we already taken to see if anything
	// is running now
	for _, backup := range childBackups {
		if backup.GetStatus().IsInProgress() {
			log.Info(
				"The system is already taking a scheduledBackup, retrying in 60 seconds",
				"backupName", backup.GetName(),
				"backupPhase", backup.GetStatus().Phase)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	return ReconcileScheduledBackup(ctx, r, log, &scheduledBackup)
}

// ReconcileScheduledBackup is the main reconciliation logic for a scheduled backup
func ReconcileScheduledBackup(
	ctx context.Context,
	client client.Client,
	log logr.Logger,
	scheduledBackup postgresqlv1alpha1.ScheduledBackupCommon,
) (ctrl.Result, error) {
	// Let's check
	schedule, err := cron.Parse(scheduledBackup.GetSchedule())
	if err != nil {
		log.Info("Detected an invalid cron schedule",
			"schedule", scheduledBackup.GetSchedule())
		return ctrl.Result{}, err
	}

	now := time.Now()

	if scheduledBackup.GetStatus().LastCheckTime == nil {
		// This is the first time we check this schedule,
		// let's wait until the first job will be actually
		// scheduled
		scheduledBackup.GetStatus().LastCheckTime = &metav1.Time{
			Time: now,
		}
		err := client.Status().Update(ctx, scheduledBackup.GetKubernetesObject())
		if err != nil {
			if apierrs.IsConflict(err) {
				// Retry later, the cache is stale
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		nextTime := schedule.Next(scheduledBackup.GetStatus().LastCheckTime.Time)
		log.Info("Next backup schedule", "next", nextTime)
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// Let's check if we are supposed to start a new backup.
	nextTime := schedule.Next(scheduledBackup.GetStatus().LastCheckTime.Time)
	log.Info("Next backup schedule", "next", nextTime)
	if now.Before(nextTime) {
		// No need to schedule a new backup, let's wait a bit
		return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
	}

	// So we have no backup running, let's create a backup.
	// Let's have deterministic names to avoid creating the job two
	// times
	name := fmt.Sprintf("%s-%d", scheduledBackup.GetName(), nextTime.Unix())
	backup := scheduledBackup.CreateBackup(name)

	log.Info("Creating backup", "backupName", backup.GetName())
	if err = client.Create(ctx, backup.GetKubernetesObject()); err != nil {
		if apierrs.IsConflict(err) {
			// Retry later, the cache is stale
			return ctrl.Result{}, nil
		}

		log.Error(
			err, "Error while creating backup object",
			"backupName", backup.GetName())
		return ctrl.Result{}, err
	}

	// Ok, now update the latest check to now
	scheduledBackup.GetStatus().LastCheckTime = &metav1.Time{
		Time: now,
	}
	scheduledBackup.GetStatus().LastScheduleTime = &metav1.Time{
		Time: nextTime,
	}
	if err = client.Status().Update(ctx, scheduledBackup.GetKubernetesObject()); err != nil {
		if apierrs.IsConflict(err) {
			// Retry later, the cache is stale
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	nextTime = schedule.Next(now)
	log.Info("Next backup schedule", "next", nextTime)
	return ctrl.Result{RequeueAfter: nextTime.Sub(now)}, nil
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

			if owner.APIVersion != apiGVString || owner.Kind != "Backup" {
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
