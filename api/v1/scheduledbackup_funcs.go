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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// IsSuspended check if a scheduled backup has been suspended or not
func (scheduledBackup ScheduledBackup) IsSuspended() bool {
	if scheduledBackup.Spec.Suspend == nil {
		return false
	}

	return *scheduledBackup.Spec.Suspend
}

// IsImmediate check if a backup has to be issued immediately upon creation or not
func (scheduledBackup ScheduledBackup) IsImmediate() bool {
	if scheduledBackup.Spec.Immediate == nil {
		return false
	}

	return *scheduledBackup.Spec.Immediate
}

// GetName gets the scheduled backup name
func (scheduledBackup *ScheduledBackup) GetName() string {
	return scheduledBackup.Name
}

// GetNamespace gets the scheduled backup name
func (scheduledBackup *ScheduledBackup) GetNamespace() string {
	return scheduledBackup.Namespace
}

// GetSchedule get the cron-like schedule of this scheduled backup
func (scheduledBackup *ScheduledBackup) GetSchedule() string {
	return scheduledBackup.Spec.Schedule
}

// GetStatus gets the status that the caller may update
func (scheduledBackup *ScheduledBackup) GetStatus() *ScheduledBackupStatus {
	return &scheduledBackup.Status
}

// CreateBackup creates a backup from this scheduled backup
func (scheduledBackup *ScheduledBackup) CreateBackup(name string) *Backup {
	backup := Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scheduledBackup.Namespace,
		},
		Spec: BackupSpec{
			Cluster:             scheduledBackup.Spec.Cluster,
			Target:              scheduledBackup.Spec.Target,
			Method:              scheduledBackup.Spec.Method,
			Online:              scheduledBackup.Spec.Online,
			OnlineConfiguration: scheduledBackup.Spec.OnlineConfiguration,
			PluginConfiguration: scheduledBackup.Spec.PluginConfiguration,
		},
	}
	utils.InheritAnnotations(&backup.ObjectMeta, scheduledBackup.Annotations, nil, configuration.Current)

	if backup.Annotations == nil {
		backup.Annotations = make(map[string]string)
	}

	if v := scheduledBackup.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName]; v != "" {
		backup.Annotations[utils.BackupVolumeSnapshotDeadlineAnnotationName] = v
	}

	return &backup
}

// SetAdmissionError sets the admission error status on the ScheduledBackup resource
func (scheduledBackup *ScheduledBackup) SetAdmissionError(msg string) {
	scheduledBackup.Status.Error = msg
}
