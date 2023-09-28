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

package utils

import (
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetadataNamespace is the annotation and label namespace used by the operator
const MetadataNamespace = "cnpg.io"

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

	// IsOnlineBackupLabelName is the name of the label used to specify whether a backup was online
	IsOnlineBackupLabelName = MetadataNamespace + "/onlineBackup"
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

	// SnapshotStartTimeAnnotationName is the name of the annotation where a snapshot's start time is kept
	SnapshotStartTimeAnnotationName = MetadataNamespace + "/snapshotStartTime"

	// SnapshotEndTimeAnnotationName is the name of the annotation where a snapshot's end time is kept
	SnapshotEndTimeAnnotationName = MetadataNamespace + "/snapshotEndTime"

	// ClusterRestartAnnotationName is the name of the annotation containing the
	// latest required restart time
	ClusterRestartAnnotationName = "kubectl.kubernetes.io/restartedAt"
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
)

// PVCRole describes the role of a PVC
type PVCRole string

const (
	// PVCRolePgData is a PVC used for storing PG_DATA
	PVCRolePgData PVCRole = "PG_DATA"
	// PVCRolePgWal is a PVC used for storing PG_WAL
	PVCRolePgWal PVCRole = "PG_WAL"
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

// IsEmptyWalArchiveCheckEnabled returns a boolean indicating if we should run the logic that checks if the WAL archive
// storage is empty
func IsEmptyWalArchiveCheckEnabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[skipEmptyWalArchiveCheck] != string(annotationStatusEnabled)
}

// MergeMap transfers the content of a giver map to a receiver
func MergeMap(receiver, giver map[string]string) {
	for key, value := range giver {
		receiver[key] = value
	}
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
