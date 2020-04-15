/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:generate=false

// ScheduledBackupCommon is implemented by every scheduled backup that
// wants to reuse the controller
type ScheduledBackupCommon interface {
	// GetName gets the scheduled backup name
	GetName() string

	// GetNamespace gets the scheduled backup namespace
	GetNamespace() string

	// GetSchedule returns the cron-like schedule of this backup
	GetSchedule() string

	// GetStatus gets the status that the caller may update
	GetStatus() *ScheduledBackupStatus

	// GetKubernetesObject gets the kubernetes object
	GetKubernetesObject() runtime.Object

	// Create a backup from this scheduled backup
	CreateBackup(name string) BackupCommon
}
