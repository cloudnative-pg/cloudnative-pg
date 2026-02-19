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

package utils

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AlphaMetadataNamespace is the annotation and label namespace used by the alpha features of
// the operator
const AlphaMetadataNamespace = "alpha.cnpg.io"

// MetadataNamespace is the annotation and label namespace used by the operator
const MetadataNamespace = "cnpg.io"

// KubernetesAppNamespaceDomain is the domain used for the official kubernetes app labels
const KubernetesAppNamespaceDomain = "app.kubernetes.io"

const (
	// ManagerName is the name of the manager for cnpg controlled objects
	ManagerName = "cloudnative-pg"

	// AppName is the name of the application
	AppName = "postgresql"

	// DatabaseComponentName is the name of the component for the database.
	DatabaseComponentName = "database"

	// PoolerComponentName is the name of the component for the pooler.
	PoolerComponentName = "pooler"
)

const (
	// KubernetesAppManagedByLabelName is the name of the label applied to all managed objects
	KubernetesAppManagedByLabelName = KubernetesAppNamespaceDomain + "/managed-by"

	// KubernetesAppLabelName is the name of the label used to indicate the name of the application
	KubernetesAppLabelName = KubernetesAppNamespaceDomain + "/name"

	// KubernetesAppInstanceLabelName is the name of the label used to indicate the unique instance of this application
	KubernetesAppInstanceLabelName = KubernetesAppNamespaceDomain + "/instance"

	// KubernetesAppVersionLabelName is the name of the label used to indicate the version postgres
	KubernetesAppVersionLabelName = KubernetesAppNamespaceDomain + "/version"

	// KubernetesAppComponentLabelName is the name of the label used to indicate the component within the architecture
	KubernetesAppComponentLabelName = KubernetesAppNamespaceDomain + "/component"
)

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

	// MajorVersionLabelName is the Postgres major version contained in a snapshot backup
	MajorVersionLabelName = MetadataNamespace + "/majorVersion"

	// PgbouncerNameLabel is the name of the label of containing the pooler name
	PgbouncerNameLabel = MetadataNamespace + "/poolerName"

	// ClusterRoleLabelName is the name of label applied to instances to mark primary/replica
	//
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

	// WatchedLabelName the name of the label which tells if a resource change will be automatically reloaded by instance
	// or not, use for Secrets or ConfigMaps
	WatchedLabelName = MetadataNamespace + "/reload"

	// UserTypeLabelName the name of the label which tells if a Secret refers
	// to a superuser database role or an application one
	UserTypeLabelName = MetadataNamespace + "/userType"

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

	// IsManagedLabelName is the name of the label used to indicate a '.spec.managed' resource
	IsManagedLabelName = MetadataNamespace + "/isManaged"

	// PluginNameLabelName is the name of the label to be applied to services
	// to have them detected as CNPG-i plugins
	PluginNameLabelName = MetadataNamespace + "/pluginName"

	// LivenessPingerAnnotationName is the name of the pinger configuration
	LivenessPingerAnnotationName = AlphaMetadataNamespace + "/livenessPinger"
)

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

	// ReconcilePodSpecAnnotationName is the name of the annotation that prevents the pod spec to be reconciled
	ReconcilePodSpecAnnotationName = MetadataNamespace + "/reconcilePodSpec"

	// HibernateClusterManifestAnnotationName contains the hibernated cluster manifest
	// Deprecated. Replaced by: ClusterManifestAnnotationName. This annotation is
	// kept for backward compatibility
	HibernateClusterManifestAnnotationName = MetadataNamespace + "/hibernateClusterManifest"

	// HibernatePgControlDataAnnotationName contains the pg_controldata output of the hibernated cluster
	// Deprecated. Replaced by: PgControldataAnnotationName. This annotation is
	// kept for backward compatibility
	HibernatePgControlDataAnnotationName = MetadataNamespace + "/hibernatePgControlData"

	// PodEnvHashAnnotationName is the name of the annotation containing the podEnvHash value
	//
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

	// SkipWalArchiving is the name of the annotation which turns off WAL archiving
	SkipWalArchiving = MetadataNamespace + "/skipWalArchiving"

	// skipEmptyWalArchiveCheck is the name of the annotation which turns off the checks that ensure that the WAL
	// archive is empty before writing data
	skipEmptyWalArchiveCheck = MetadataNamespace + "/skipEmptyWalArchiveCheck"

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

	// BackupVolumeSnapshotDeadlineAnnotationName is the annotation for the snapshot backup failure deadline in minutes.
	// It is only applied to snapshot retryable errors
	BackupVolumeSnapshotDeadlineAnnotationName = MetadataNamespace + "/volumeSnapshotDeadline"

	// SnapshotStartTimeAnnotationName is the name of the annotation where a snapshot's start time is kept
	SnapshotStartTimeAnnotationName = MetadataNamespace + "/snapshotStartTime"

	// SnapshotEndTimeAnnotationName is the name of the annotation where a snapshot's end time is kept
	SnapshotEndTimeAnnotationName = MetadataNamespace + "/snapshotEndTime"

	// ClusterRestartAnnotationName is the name of the annotation containing the
	// latest required restart time
	ClusterRestartAnnotationName = "kubectl.kubernetes.io/restartedAt"

	// UpdateStrategyAnnotation is the name of the annotation used to indicate how to update the given resource
	UpdateStrategyAnnotation = MetadataNamespace + "/updateStrategy"

	// PluginClientSecretAnnotationName is the name of the annotation containing
	// the secret containing the TLS credentials that the operator should use to
	// connect to the plugin
	PluginClientSecretAnnotationName = MetadataNamespace + "/pluginClientSecret"

	// PluginServerSecretAnnotationName is the name of the annotation containing
	// the secret containing the TLS credentials that are used by the plugin
	// server to authenticate
	PluginServerSecretAnnotationName = MetadataNamespace + "/pluginServerSecret"

	// PluginPortAnnotationName is the name of the annotation containing the
	// port the plugin is listening to
	PluginPortAnnotationName = MetadataNamespace + "/pluginPort"

	// PluginServerNameAnnotationName is the name of the annotation containing the
	// server name to use for TLS verification when connecting to the plugin.
	// If not specified, defaults to the service name
	PluginServerNameAnnotationName = MetadataNamespace + "/pluginServerName"

	// PodPatchAnnotationName is the name of the annotation containing the
	// patch to apply to the pod
	PodPatchAnnotationName = MetadataNamespace + "/podPatch"

	// InitDBJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the initdb jobs
	InitDBJobPatchAnnotationName = MetadataNamespace + "/initdbJobPatch"

	// ImportJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the import jobs
	ImportJobPatchAnnotationName = MetadataNamespace + "/importJobPatch"

	// PGBaseBackupJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the pgbasebackup jobs
	PGBaseBackupJobPatchAnnotationName = MetadataNamespace + "/pgbasebackupJobPatch"

	// FullRecoveryJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the full-recovery jobs
	FullRecoveryJobPatchAnnotationName = MetadataNamespace + "/fullRecoveryJobPatch"

	// JoinJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the join jobs
	JoinJobPatchAnnotationName = MetadataNamespace + "/joinJobPatch"

	// SnapshotRecoveryJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the snapshot-recovery jobs
	SnapshotRecoveryJobPatchAnnotationName = MetadataNamespace + "/snapshotRecoveryJobPatch"

	// MajorUpgradeJobPatchAnnotationName is the annotation containing the JSON patch
	// to apply to the major upgrade jobs
	MajorUpgradeJobPatchAnnotationName = MetadataNamespace + "/majorUpgradeJobPatch"

	// WebhookValidationAnnotationName is the name of the annotation describing if
	// the validation webhook should be enabled or disabled
	WebhookValidationAnnotationName = MetadataNamespace + "/validation"

	// FailoverQuorumAnnotationName is the name of the annotation that allows the
	// user to enable synchronous quorum failover protection.
	//
	// This feature enables quorum-based check before failover, ensuring
	// no data loss at the expense of availability.
	FailoverQuorumAnnotationName = AlphaMetadataNamespace + "/failoverQuorum"

	// EnableInstancePprofAnnotationName is the name of the annotation describing if the instances generated by
	// the cluster should enable the pprof server. Accepts "true" | "false" values.
	EnableInstancePprofAnnotationName = AlphaMetadataNamespace + "/enableInstancePprof"

	// UnrecoverableInstanceAnnotationName is the name of the annotation telling the
	// operator if a instance is recoverable or not. Not recoverable instances will
	// be deleted with the contents of their PVCs.
	UnrecoverableInstanceAnnotationName = AlphaMetadataNamespace + "/unrecoverable"
)

type annotationStatus string

const (
	annotationStatusDisabled annotationStatus = "disabled"
	annotationStatusEnabled  annotationStatus = "enabled"
)

// PodRole describes the Role of a given pod
type PodRole string

const (
	// PodRoleInstance the label value indicating an instance
	PodRoleInstance PodRole = "instance"
	// PodRolePooler the label value indicating a pooler instance
	PodRolePooler PodRole = "pooler"
)

// PVCRole describes the role of a PVC
type PVCRole string

const (
	// PVCRolePgData the label value for the data PVC role
	PVCRolePgData PVCRole = "PG_DATA"
	// PVCRolePgWal the label value for the wal PVC role
	PVCRolePgWal PVCRole = "PG_WAL"
	// PVCRolePgTablespace the label value for the tablespace PVC role
	PVCRolePgTablespace PVCRole = "PG_TABLESPACE"
)

// HibernationAnnotationValue describes the status of the hibernation
type HibernationAnnotationValue string

const (
	// HibernationAnnotationValueOff is the value of hibernation annotation when the hibernation
	// has been deactivated for the cluster
	HibernationAnnotationValueOff HibernationAnnotationValue = "off"

	// HibernationAnnotationValueOn is the value of hibernation annotation when the hibernation
	// has been requested for the cluster
	HibernationAnnotationValueOn HibernationAnnotationValue = "on"
)

// UserType tells if a secret refers to a superuser database role
// or an application one
type UserType string

const (
	// UserTypeSuperuser is the type of a superuser database
	// role
	UserTypeSuperuser UserType = "superuser"

	// UserTypeApp is the type of an application role
	UserTypeApp UserType = "app"
)

// LabelClusterName labels the object with the cluster name
func LabelClusterName(object *metav1.ObjectMeta, name string) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	object.Labels[ClusterLabelName] = name
}

// SetOperatorVersion set inside a certain object metadata the annotation
// containing the version of the operator that generated the object
func SetOperatorVersion(object *metav1.ObjectMeta, version string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	object.Annotations[OperatorVersionAnnotationName] = version
}

// InheritanceController controls if a label or an annotation should be
// inherited
type InheritanceController interface {
	// IsAnnotationInherited checks if a certain annotation should be
	// inherited
	IsAnnotationInherited(name string) bool

	// IsLabelInherited checks if a certain label should be
	// inherited
	IsLabelInherited(name string) bool
}

// InheritAnnotations puts into the object metadata the passed annotations if
// the annotations are supposed to be inherited. The passed configuration is
// used to determine whenever a certain annotation is inherited or not
func InheritAnnotations(
	object *metav1.ObjectMeta,
	annotations map[string]string,
	fixedAnnotations map[string]string,
	controller InheritanceController,
) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	for key, value := range fixedAnnotations {
		object.Annotations[key] = value
	}

	for key, value := range annotations {
		if controller.IsAnnotationInherited(key) {
			object.Annotations[key] = value
		}
	}
}

// InheritLabels puts into the object metadata the passed labels if
// the labels are supposed to be inherited. The passed configuration is
// used to determine whenever a certain label is inherited or not
func InheritLabels(
	object *metav1.ObjectMeta,
	labels map[string]string,
	fixedLabels map[string]string,
	controller InheritanceController,
) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	for key, value := range fixedLabels {
		object.Labels[key] = value
	}

	for key, value := range labels {
		if controller.IsLabelInherited(key) {
			object.Labels[key] = value
		}
	}
}

func getAnnotationAppArmor(spec *corev1.PodSpec, annotations map[string]string) map[string]string {
	containsContainerWithName := func(name string, containers ...corev1.Container) bool {
		for _, container := range containers {
			if container.Name == name {
				return true
			}
		}

		return false
	}

	appArmorAnnotations := make(map[string]string)
	for annotation, value := range annotations {
		if strings.HasPrefix(annotation, AppArmorAnnotationPrefix) {
			appArmorSplit := strings.SplitN(annotation, "/", 2)
			if len(appArmorSplit) < 2 {
				continue
			}

			containerName := appArmorSplit[1]
			if containsContainerWithName(containerName, append(spec.Containers, spec.InitContainers...)...) {
				appArmorAnnotations[annotation] = value
			}
		}
	}
	return appArmorAnnotations
}

// IsAnnotationAppArmorPresent checks if one of the annotations is an AppArmor annotation
func IsAnnotationAppArmorPresent(spec *corev1.PodSpec, annotations map[string]string) bool {
	annotation := getAnnotationAppArmor(spec, annotations)
	return len(annotation) != 0
}

// IsAnnotationAppArmorPresentInObject checks if the AppArmor annotations are present or not in the given Object
func IsAnnotationAppArmorPresentInObject(
	object *metav1.ObjectMeta,
	spec *corev1.PodSpec,
	annotations map[string]string,
) bool {
	objAnnotations := getAnnotationAppArmor(spec, object.Annotations)
	appArmorAnnotations := getAnnotationAppArmor(spec, annotations)
	return reflect.DeepEqual(objAnnotations, appArmorAnnotations)
}

// AnnotateAppArmor adds an annotation to the pod
func AnnotateAppArmor(object *metav1.ObjectMeta, spec *corev1.PodSpec, annotations map[string]string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}
	appArmorAnnotations := getAnnotationAppArmor(spec, annotations)
	for annotation, value := range appArmorAnnotations {
		object.Annotations[annotation] = value
	}
}

// IsReconciliationDisabled checks if the reconciliation loop is disabled on the given resource
func IsReconciliationDisabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[ReconciliationLoopAnnotationName] == string(annotationStatusDisabled)
}

// IsPodSpecReconciliationDisabled checks if the pod spec reconciliation is disabled
func IsPodSpecReconciliationDisabled(object *metav1.ObjectMeta) bool {
	if object.Annotations == nil {
		return false
	}
	return object.Annotations[ReconcilePodSpecAnnotationName] == string(annotationStatusDisabled)
}

// IsEmptyWalArchiveCheckEnabled returns a boolean indicating if we should run the logic that checks if the WAL archive
// storage is empty
func IsEmptyWalArchiveCheckEnabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[skipEmptyWalArchiveCheck] != string(annotationStatusEnabled)
}

// IsWalArchivingDisabled returns a boolean indicating if PostgreSQL not archive
// WAL files
func IsWalArchivingDisabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[SkipWalArchiving] == string(annotationStatusEnabled)
}

// GetInstanceRole tries to fetch the ClusterRoleLabelName andClusterInstanceRoleLabelName value from a given labels map
func GetInstanceRole(labels map[string]string) (string, bool) {
	if value := labels[ClusterRoleLabelName]; value != "" {
		return value, true
	}
	if value := labels[ClusterInstanceRoleLabelName]; value != "" {
		return value, true
	}

	return "", false
}

// SetInstanceRole sets both ClusterRoleLabelName and ClusterInstanceRoleLabelName on the given ObjectMeta
func SetInstanceRole(meta metav1.ObjectMeta, role string) {
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[ClusterRoleLabelName] = role
	meta.Labels[ClusterInstanceRoleLabelName] = role
}

// MergeObjectsMetadata is capable of merging the labels and annotations of two objects metadata
func MergeObjectsMetadata(receiver client.Object, giver client.Object) {
	if receiver.GetLabels() == nil {
		receiver.SetLabels(map[string]string{})
	}
	if receiver.GetAnnotations() == nil {
		receiver.SetAnnotations(map[string]string{})
	}

	maps.Copy(receiver.GetLabels(), giver.GetLabels())
	maps.Copy(receiver.GetAnnotations(), giver.GetAnnotations())
}

// GetClusterSerialValue returns the `nodeSerial` value from the given annotation map or return an error
func GetClusterSerialValue(annotations map[string]string) (int, error) {
	rawSerial, ok := annotations[ClusterSerialAnnotationName]
	if !ok {
		return 0, fmt.Errorf("no serial annotation found")
	}

	serial, err := strconv.Atoi(rawSerial)
	if err != nil {
		return 0, fmt.Errorf("invalid serial annotation found: %w", err)
	}

	return serial, nil
}

// GetJobPatchAnnotationForRole returns the appropriate job patch annotation name
// for the given job role string. Returns an empty string if the role is unknown.
func GetJobPatchAnnotationForRole(role string) string {
	switch role {
	case "import":
		return ImportJobPatchAnnotationName
	case "initdb":
		return InitDBJobPatchAnnotationName
	case "pgbasebackup":
		return PGBaseBackupJobPatchAnnotationName
	case "full-recovery":
		return FullRecoveryJobPatchAnnotationName
	case "join":
		return JoinJobPatchAnnotationName
	case "snapshot-recovery":
		return SnapshotRecoveryJobPatchAnnotationName
	case "major-upgrade":
		return MajorUpgradeJobPatchAnnotationName
	default:
		return ""
	}
}

// KnownJobPatchAnnotations returns a slice of all known job patch annotation names.
// This is useful for validation purposes.
func KnownJobPatchAnnotations() []string {
	return []string{
		InitDBJobPatchAnnotationName,
		ImportJobPatchAnnotationName,
		PGBaseBackupJobPatchAnnotationName,
		FullRecoveryJobPatchAnnotationName,
		JoinJobPatchAnnotationName,
		SnapshotRecoveryJobPatchAnnotationName,
		MajorUpgradeJobPatchAnnotationName,
	}
}
