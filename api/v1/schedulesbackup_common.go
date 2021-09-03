/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	GetKubernetesObject() client.Object

	// Create a backup from this scheduled backup
	CreateBackup(name string) BackupCommon

	// IsImmediate returns whether a backup should be started upon creation or not
	IsImmediate() bool

	// IsSuspended returns whether the backup is suspended or not
	IsSuspended() bool
}
