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

const (
	// OperatorVersionAnnotationName is the name of the annotation containing
	// the version of the operator that generated a certain object
	OperatorVersionAnnotationName = MetadataNamespace + "/operatorVersion"

	// AppArmorAnnotationPrefix will be the name of the AppArmor profile to apply
	// This is required for Azure but can be set in other environments
	AppArmorAnnotationPrefix = "container.apparmor.security.beta.kubernetes.io"

	// ReconciliationLoopAnnotationName is the name of the annotation controlling
	// the status of the reconciliation loop for the cluster
	ReconciliationLoopAnnotationName = MetadataNamespace + "/reconciliationLoop"

	// HibernateClusterManifestAnnotationName contains the hibernated cluster manifest
	// Deprecated. Replaced by: ClusterManifestAnnotationName. This annotation is
	// kept for backward compatibility
	HibernateClusterManifestAnnotationName = MetadataNamespace + "/hibernateClusterManifest"

	// HibernatePgControlDataAnnotationName contains the pg_controldata output of the hibernated cluster
	// Deprecated. Replaced by: PgControldataAnnotationName. This annotation is
	// kept for backward compatibility
	HibernatePgControlDataAnnotationName = MetadataNamespace + "/hibernatePgControlData"

	// PodEnvHashAnnotationName is the name of the annotation containing the podEnvHash value
	// Deprecated: the PodSpec annotation covers the environment drift. This annotation is
	// kept for backward compatibility
	PodEnvHashAnnotationName = MetadataNamespace + "/podEnvHash"

	// PodSpecAnnotationName is the name of the annotation with the PodSpec derived from the cluster
	PodSpecAnnotationName = MetadataNamespace + "/podSpec"

	// ClusterManifestAnnotationName is the name of the annotation containing the cluster manifest
	ClusterManifestAnnotationName = MetadataNamespace + "/clusterManifest"

	// CoredumpFilter stores the value defined by the user to set in /proc/self/coredump_filter
	CoredumpFilter = MetadataNamespace + "/coredumpFilter"

	// PgControldataAnnotationName is the name of the annotation containing the pg_controldata output of the cluster
	PgControldataAnnotationName = MetadataNamespace + "/pgControldata"

	// SkipEmptyWalArchiveCheck is the name of the annotation which turns off the checks that ensure that the WAL
	// archive is empty before writing data
	SkipEmptyWalArchiveCheck = MetadataNamespace + "/skipEmptyWalArchiveCheck"

	// ClusterSerialAnnotationName is the name of the annotation containing the
	// serial number of the node
	ClusterSerialAnnotationName = MetadataNamespace + "/nodeSerial"

	// ClusterReloadAnnotationName is the name of the annotation containing the
	// latest reload time trigger by external
	ClusterReloadAnnotationName = MetadataNamespace + "/reloadedAt"

	// PVCStatusAnnotationName is the name of the annotation that shows the current status of the PVC.
	// The status can be "initializing", "ready" or "detached"
	PVCStatusAnnotationName = MetadataNamespace + "/pvcStatus"

	// LegacyBackupAnnotationName is the name of the annotation represents whether taking a backup without passing
	// the name argument even on barman version 3.3.0+. The value can be "true" or "false"
	LegacyBackupAnnotationName = MetadataNamespace + "/forceLegacyBackup"

	// HibernationAnnotationName is the name of the annotation which used to declaratively hibernate a
	// PostgreSQL cluster
	HibernationAnnotationName = MetadataNamespace + "/hibernation"

	// PoolerSpecHashAnnotationName is the name of the annotation added to the deployment to tell
	// the hash of the Pooler Specification
	PoolerSpecHashAnnotationName = MetadataNamespace + "/poolerSpecHash"

	// OperatorManagedSecretsAnnotationName is the name of the annotation containing
	// the secrets managed by the operator inside the generated service account
	OperatorManagedSecretsAnnotationName = MetadataNamespace + "/managedSecrets"

	// FencedInstanceAnnotation is the annotation to be used for fencing instances, the value should be a
	// JSON list of all the instances we want to be fenced, e.g. `["cluster-example-1","cluster-example-2`"].
	// If the list contain the "*" element, every node is fenced.
	FencedInstanceAnnotation = MetadataNamespace + "/fencedInstances"

	// CNPGHashAnnotationName is the name of the annotation containing the hash of the resource used by operator
	// expect the pooler that uses PoolerSpecHashAnnotationName
	CNPGHashAnnotationName = MetadataNamespace + "/hash"

	// BackupStartWALAnnotationName is the name of the annotation where a backup's start WAL is kept
	BackupStartWALAnnotationName = MetadataNamespace + "/backupStartWAL"

	// BackupEndWALAnnotationName is the name of the annotation where a backup's end WAL is kept
	BackupEndWALAnnotationName = MetadataNamespace + "/backupEndWAL"

	// BackupStartTimeAnnotationName is the name of the annotation where a backup's start time is kept
	BackupStartTimeAnnotationName = MetadataNamespace + "/backupStartTime"

	// BackupEndTimeAnnotationName is the name of the annotation where a backup's end time is kept
	BackupEndTimeAnnotationName = MetadataNamespace + "/backupEndTime"

	// BackupLabelFileAnnotationName is the name of the annotation where the `backup_label` file is kept
	BackupLabelFileAnnotationName = MetadataNamespace + "/backupLabelFile"

	// BackupTablespaceMapFileAnnotationName is the name of the annotation where the `tablespace_map` file is kept
	BackupTablespaceMapFileAnnotationName = MetadataNamespace + "/backupTablespaceMapFile"

	// SnapshotStartTimeAnnotationName is the name of the annotation where a snapshot's start time is kept
	SnapshotStartTimeAnnotationName = MetadataNamespace + "/snapshotStartTime"

	// SnapshotEndTimeAnnotationName is the name of the annotation where a snapshot's end time is kept
	SnapshotEndTimeAnnotationName = MetadataNamespace + "/snapshotEndTime"

	// ClusterRestartAnnotationName is the name of the annotation containing the
	// latest required restart time
	ClusterRestartAnnotationName = "kubectl.kubernetes.io/restartedAt"
)
