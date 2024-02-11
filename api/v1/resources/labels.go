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

package resources

// When you add a new label or annotation, please make sure that you also update the
// publicly visible documentation, namely the `docs/src/labels_annotations.md` file
const (
	// ClusterLabelName is the name of the label cluster which the backup CR belongs to
	ClusterLabelName = MetadataNamespace + "/cluster"

	// JobRoleLabelName is the name of the label containing the purpose of the executed job
	// the value could be import, initdb, join
	JobRoleLabelName = MetadataNamespace + "/jobRole"

	// PvcRoleLabelName is the name of the label containing the purpose of the pvc
	PvcRoleLabelName = MetadataNamespace + "/pvcRole"

	// TablespaceNameLabelName is the name of the label containing tablespace name that a pvc holds
	TablespaceNameLabelName = "cnpg.io/tablespaceName"

	// PodRoleLabelName is the name of the label containing the podRole value
	PodRoleLabelName = MetadataNamespace + "/podRole"

	// InstanceNameLabelName is the name of the label containing the instance name
	InstanceNameLabelName = MetadataNamespace + "/instanceName"

	// BackupNameLabelName is the name of the label containing the backup id, available on backup resources
	BackupNameLabelName = MetadataNamespace + "/backupName"

	// PgbouncerNameLabel is the name of the label of containing the pooler name
	PgbouncerNameLabel = MetadataNamespace + "/poolerName"

	// ClusterRoleLabelName is the name of label applied to instances to mark primary/replica
	// Deprecated: Use ClusterInstanceRoleLabelName.
	ClusterRoleLabelName = "role"

	// ClusterInstanceRoleLabelName is the name of label applied to instances to mark primary/replica
	ClusterInstanceRoleLabelName = MetadataNamespace + "/instanceRole"

	// ImmediateBackupLabelName is the name of the label applied to backups to tell if the first scheduled backup is
	// taken immediately or not
	ImmediateBackupLabelName = MetadataNamespace + "/immediateBackup"

	// ParentScheduledBackupLabelName is the name of the label applied to backups to easily tell the name of parent
	// scheduled backup if a backup is created by a scheduled backup
	ParentScheduledBackupLabelName = MetadataNamespace + "/scheduled-backup"

	// WatchedLabelName the name of the label which tell if a resource change will be automatically reloaded by instance
	// or not, use for Secrets or ConfigMaps
	WatchedLabelName = MetadataNamespace + "/reload"

	// BackupTimelineLabelName is the name or the label where the timeline of a backup is kept
	BackupTimelineLabelName = MetadataNamespace + "/backupTimeline"

	// BackupYearLabelName is the name of the label where the year of a backup is kept
	BackupYearLabelName = MetadataNamespace + "/backupYear"

	// BackupMonthLabelName is the name of the label where the month of a backup is kept
	BackupMonthLabelName = MetadataNamespace + "/backupMonth"

	// BackupDateLabelName is the name of the label where the date of a backup in 'YYYYMMDD' format is kept
	BackupDateLabelName = MetadataNamespace + "/backupDate"

	// IsOnlineBackupLabelName is the name of the label used to specify whether a backup was online
	IsOnlineBackupLabelName = MetadataNamespace + "/onlineBackup"
)
