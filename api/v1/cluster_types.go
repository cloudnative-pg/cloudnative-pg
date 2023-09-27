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

package v1

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

const (
	// PrimaryPodDisruptionBudgetSuffix is the suffix appended to the cluster name
	// to get the name of the PDB used for the cluster primary
	PrimaryPodDisruptionBudgetSuffix = "-primary"

	// ReplicationSecretSuffix is the suffix appended to the cluster name to
	// get the name of the generated replication secret for PostgreSQL
	ReplicationSecretSuffix = "-replication" // #nosec

	// SuperUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the PostgreSQL superuser secret
	SuperUserSecretSuffix = "-superuser"

	// ApplicationUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the application user secret
	ApplicationUserSecretSuffix = "-app"

	// DefaultServerCaSecretSuffix is the suffix appended to the secret containing
	// the generated CA for the cluster
	DefaultServerCaSecretSuffix = "-ca"

	// ClientCaSecretSuffix is the suffix appended to the secret containing
	// the generated CA for the client certificates
	ClientCaSecretSuffix = "-ca"

	// ServerSecretSuffix is the suffix appended to the secret containing
	// the generated server secret for PostgreSQL
	ServerSecretSuffix = "-server"

	// ServiceAnySuffix is the suffix appended to the cluster name to get the
	// service name for every node (including non-ready ones)
	ServiceAnySuffix = "-any"

	// ServiceReadSuffix is the suffix appended to the cluster name to get the
	// service name for every ready node that you can use to read data (including the primary)
	ServiceReadSuffix = "-r"

	// ServiceReadOnlySuffix is the suffix appended to the cluster name to get the
	// service name for every ready node that you can use to read data (excluding the primary)
	ServiceReadOnlySuffix = "-ro"

	// ServiceReadWriteSuffix is the suffix appended to the cluster name to get
	// the se service name for every node that you can use to read and write
	// data
	ServiceReadWriteSuffix = "-rw"

	// ClusterSecretSuffix is the suffix appended to the cluster name to
	// get the name of the pull secret
	ClusterSecretSuffix = "-pull-secret"

	// WalArchiveVolumeSuffix is the suffix appended to the instance name to
	// get the name of the PVC dedicated to WAL files.
	WalArchiveVolumeSuffix = "-wal"

	// StreamingReplicationUser is the name of the user we'll use for
	// streaming replication purposes
	StreamingReplicationUser = "streaming_replica"

	// defaultPostgresUID is the default UID which is used by PostgreSQL
	defaultPostgresUID = 26

	// defaultPostgresGID is the default GID which is used by PostgreSQL
	defaultPostgresGID = 26

	// PodAntiAffinityTypeRequired is the label for required anti-affinity type
	PodAntiAffinityTypeRequired = "required"

	// PodAntiAffinityTypePreferred is the label for preferred anti-affinity type
	PodAntiAffinityTypePreferred = "preferred"

	// DefaultPgBouncerPoolerSecretSuffix is the suffix for the default pgbouncer Pooler secret
	DefaultPgBouncerPoolerSecretSuffix = "-pooler"

	// PendingFailoverMarker is used as target primary to signal that a failover is required
	PendingFailoverMarker = "pending"

	// PGBouncerPoolerUserName is the name of the role to be used for
	PGBouncerPoolerUserName = "cnpg_pooler_pgbouncer"
)

// SnapshotOwnerReference defines the reference type for the owner of the snapshot.
// This specifies which owner the processed resources should relate to.
type SnapshotOwnerReference string

// Constants to represent the allowed types for SnapshotOwnerReference.
const (
	// ShapshotOwnerReferenceNone indicates that the snapshot does not have any owner reference.
	ShapshotOwnerReferenceNone SnapshotOwnerReference = "none"
	// SnapshotOwnerReferenceBackup indicates that the snapshot is owned by the backup resource.
	SnapshotOwnerReferenceBackup SnapshotOwnerReference = "backup"
	// SnapshotOwnerReferenceCluster indicates that the snapshot is owned by the cluster resource.
	SnapshotOwnerReferenceCluster SnapshotOwnerReference = "cluster"
)

// VolumeSnapshotConfiguration represents the configuration for the execution of snapshot backups.
type VolumeSnapshotConfiguration struct {
	// Labels are key-value pairs that will be added to .metadata.labels snapshot resources.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations key-value pairs that will be added to .metadata.annotations snapshot resources.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// ClassName specifies the Snapshot Class to be used for PG_DATA PersistentVolumeClaim.
	// It is the default class for the other types if no specific class is present
	// +optional
	ClassName string `json:"className,omitempty"`
	// WalClassName specifies the Snapshot Class to be used for the PG_WAL PersistentVolumeClaim.
	// +optional
	WalClassName string `json:"walClassName,omitempty"`
	// SnapshotOwnerReference indicates the type of owner reference the snapshot should have
	// +optional
	// +kubebuilder:validation:Enum:=none;cluster;backup
	// +kubebuilder:default:=none
	SnapshotOwnerReference SnapshotOwnerReference `json:"snapshotOwnerReference,omitempty"`
}

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Description of this PostgreSQL cluster
	// +optional
	Description string `json:"description,omitempty"`

	// Metadata that will be inherited by all objects related to the Cluster
	// +optional
	InheritedMetadata *EmbeddedObjectMetadata `json:"inheritedMetadata,omitempty"`

	// Name of the container image, supporting both tags (`<image>:<tag>`)
	// and digests for deterministic and repeatable deployments
	// (`<image>:<tag>@sha256:<digestValue>`)
	// +optional
	ImageName string `json:"imageName,omitempty"`

	// Image pull policy.
	// One of `Always`, `Never` or `IfNotPresent`.
	// If not defined, it defaults to `IfNotPresent`.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/containers/images#updating-images
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// If specified, the pod will be dispatched by specified Kubernetes
	// scheduler. If not specified, the pod will be dispatched by the default
	// scheduler. More info:
	// https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`

	// The UID of the `postgres` user inside the image, defaults to `26`
	// +kubebuilder:default:=26
	// +optional
	PostgresUID int64 `json:"postgresUID,omitempty"`

	// The GID of the `postgres` user inside the image, defaults to `26`
	// +kubebuilder:default:=26
	// +optional
	PostgresGID int64 `json:"postgresGID,omitempty"`

	// Number of instances required in the cluster
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	Instances int `json:"instances"`

	// Minimum number of instances required in synchronous replication with the
	// primary. Undefined or 0 allow writes to complete when no standby is
	// available.
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinSyncReplicas int `json:"minSyncReplicas,omitempty"`

	// The target value for the synchronous replication quorum, that can be
	// decreased if the number of ready standbys is lower than this.
	// Undefined or 0 disable synchronous replication.
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxSyncReplicas int `json:"maxSyncReplicas,omitempty"`

	// Configuration of the PostgreSQL server
	// +optional
	PostgresConfiguration PostgresConfiguration `json:"postgresql,omitempty"`

	// Replication slots management configuration
	// +kubebuilder:default:={"highAvailability":{"enabled":true}}
	// +optional
	ReplicationSlots *ReplicationSlotsConfiguration `json:"replicationSlots,omitempty"`

	// Instructions to bootstrap this cluster
	// +optional
	Bootstrap *BootstrapConfiguration `json:"bootstrap,omitempty"`

	// Replica cluster configuration
	// +optional
	ReplicaCluster *ReplicaClusterConfiguration `json:"replica,omitempty"`

	// The secret containing the superuser password. If not defined a new
	// secret will be created with a randomly generated password
	// +optional
	SuperuserSecret *LocalObjectReference `json:"superuserSecret,omitempty"`

	// When this option is enabled, the operator will use the `SuperuserSecret`
	// to update the `postgres` user password (if the secret is
	// not present, the operator will automatically create one). When this
	// option is disabled, the operator will ignore the `SuperuserSecret` content, delete
	// it when automatically created, and then blank the password of the `postgres`
	// user by setting it to `NULL`. Disabled by default.
	// +kubebuilder:default:=false
	// +optional
	EnableSuperuserAccess *bool `json:"enableSuperuserAccess,omitempty"`

	// The configuration for the CA and related certificates
	// +optional
	Certificates *CertificatesConfiguration `json:"certificates,omitempty"`

	// The list of pull secrets to be used to pull the images
	// +optional
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Configuration of the storage of the instances
	// +optional
	StorageConfiguration StorageConfiguration `json:"storage,omitempty"`

	// Configure the generation of the service account
	// +optional
	ServiceAccountTemplate *ServiceAccountTemplate `json:"serviceAccountTemplate,omitempty"`

	// Configuration of the storage for PostgreSQL WAL (Write-Ahead Log)
	// +optional
	WalStorage *StorageConfiguration `json:"walStorage,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance to
	// successfully start up (default 3600).
	// The startup probe failure threshold is derived from this value using the formula:
	// ceiling(startDelay / 10).
	// +kubebuilder:default:=3600
	// +optional
	MaxStartDelay int32 `json:"startDelay,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance to
	// gracefully shutdown (default 1800)
	// +kubebuilder:default:=1800
	// +optional
	MaxStopDelay int32 `json:"stopDelay,omitempty"`

	// The time in seconds that controls the window of time reserved for the smart shutdown of Postgres to complete.
	// this formula to compute the timeout of smart shutdown is `max(stopDelay -  smartStopDelay, 30)`
	// +kubebuilder:default:=180
	// +optional
	SmartStopDelay int32 `json:"smartStopDelay,omitempty"`

	// The time in seconds that is allowed for a primary PostgreSQL instance
	// to gracefully shutdown during a switchover.
	// Default value is 3600 seconds (1 hour).
	// +kubebuilder:default:=3600
	// +optional
	MaxSwitchoverDelay int32 `json:"switchoverDelay,omitempty"`

	// The amount of time (in seconds) to wait before triggering a failover
	// after the primary PostgreSQL instance in the cluster was detected
	// to be unhealthy
	// +kubebuilder:default:=0
	// +optional
	FailoverDelay int32 `json:"failoverDelay,omitempty"`

	// Affinity/Anti-affinity rules for Pods
	// +optional
	Affinity AffinityConfiguration `json:"affinity,omitempty"`

	// TopologySpreadConstraints specifies how to spread matching pods among the given topology.
	// More info:
	// https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Resources requirements of every generated Pod. Please refer to
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// for more information.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Name of the priority class which will be used in every generated Pod, if the PriorityClass
	// specified does not exist, the pod will not be able to schedule.  Please refer to
	// https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass
	// for more information
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Deployment strategy to follow to upgrade the primary server during a rolling
	// update procedure, after all replicas have been successfully updated:
	// it can be automated (`unsupervised` - default) or manual (`supervised`)
	// +kubebuilder:default:=unsupervised
	// +kubebuilder:validation:Enum:=unsupervised;supervised
	// +optional
	PrimaryUpdateStrategy PrimaryUpdateStrategy `json:"primaryUpdateStrategy,omitempty"`

	// Method to follow to upgrade the primary server during a rolling
	// update procedure, after all replicas have been successfully updated:
	// it can be with a switchover (`switchover`) or in-place (`restart` - default)
	// +kubebuilder:default:=restart
	// +kubebuilder:validation:Enum:=switchover;restart
	// +optional
	PrimaryUpdateMethod PrimaryUpdateMethod `json:"primaryUpdateMethod,omitempty"`

	// The configuration to be used for backups
	// +optional
	Backup *BackupConfiguration `json:"backup,omitempty"`

	// Define a maintenance window for the Kubernetes nodes
	// +optional
	NodeMaintenanceWindow *NodeMaintenanceWindow `json:"nodeMaintenanceWindow,omitempty"`

	// The configuration of the monitoring infrastructure of this cluster
	// +optional
	Monitoring *MonitoringConfiguration `json:"monitoring,omitempty"`

	// The list of external clusters which are used in the configuration
	// +optional
	ExternalClusters []ExternalCluster `json:"externalClusters,omitempty"`

	// The instances' log level, one of the following values: error, warning, info (default), debug, trace
	// +kubebuilder:default:=info
	// +kubebuilder:validation:Enum:=error;warning;info;debug;trace
	// +optional
	LogLevel string `json:"logLevel,omitempty"`

	// Template to be used to define projected volumes, projected volumes will be mounted
	// under `/projected` base folder
	// +optional
	ProjectedVolumeTemplate *corev1.ProjectedVolumeSource `json:"projectedVolumeTemplate,omitempty"`

	// Env follows the Env format to pass environment variables
	// to the pods created in the cluster
	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom follows the EnvFrom format to pass environment variables
	// sources to the pods to be used by Env
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// The configuration that is used by the portions of PostgreSQL that are managed by the instance manager
	// +optional
	Managed *ManagedConfiguration `json:"managed,omitempty"`

	// The SeccompProfile applied to every Pod and Container.
	// Defaults to: `RuntimeDefault`
	// +optional
	SeccompProfile *corev1.SeccompProfile `json:"seccompProfile,omitempty"`
}

const (
	// PhaseSwitchover when a cluster is changing the primary node
	PhaseSwitchover = "Switchover in progress"

	// PhaseFailOver in case a pod is missing and need to change primary
	PhaseFailOver = "Failing over"

	// PhaseFirstPrimary for an starting cluster
	PhaseFirstPrimary = "Setting up primary"

	// PhaseCreatingReplica everytime we add a new replica
	PhaseCreatingReplica = "Creating a new replica"

	// PhaseUpgrade upgrade in process
	PhaseUpgrade = "Upgrading cluster"

	// PhaseWaitingForUser set the status to wait for an action from the user
	PhaseWaitingForUser = "Waiting for user action"

	// PhaseInplacePrimaryRestart for a cluster restarting the primary instance in-place
	PhaseInplacePrimaryRestart = "Primary instance is being restarted in-place"

	// PhaseInplaceDeletePrimaryRestart for a cluster restarting the primary instance without a switchover
	PhaseInplaceDeletePrimaryRestart = "Primary instance is being restarted without a switchover"

	// PhaseHealthy for a cluster doing nothing
	PhaseHealthy = "Cluster in healthy state"

	// PhaseUnrecoverable for an unrecoverable cluster
	PhaseUnrecoverable = "Cluster is in an unrecoverable state, needs manual intervention"

	// PhaseWaitingForInstancesToBeActive is a waiting phase that is triggered when an instance pod is not active
	PhaseWaitingForInstancesToBeActive = "Waiting for the instances to become active"

	// PhaseOnlineUpgrading for when the instance manager is being upgraded in place
	PhaseOnlineUpgrading = "Online upgrade in progress"

	// PhaseApplyingConfiguration is set by the instance manager when a configuration
	// change is being detected
	PhaseApplyingConfiguration = "Applying configuration"
)

// ServiceAccountTemplate contains the template needed to generate the service accounts
type ServiceAccountTemplate struct {
	// Metadata are the metadata to be used for the generated
	// service account
	Metadata Metadata `json:"metadata"`
}

// MergeMetadata adds the passed custom annotations and labels in the service account.
func (st *ServiceAccountTemplate) MergeMetadata(sa *corev1.ServiceAccount) {
	if st == nil {
		return
	}
	if sa.Labels == nil {
		sa.Labels = map[string]string{}
	}
	if sa.Annotations == nil {
		sa.Annotations = map[string]string{}
	}

	utils.MergeMap(sa.Labels, st.Metadata.Labels)
	utils.MergeMap(sa.Annotations, st.Metadata.Annotations)
}

// PodTopologyLabels represent the topology of a Pod. map[labelName]labelValue
type PodTopologyLabels map[string]string

// matchesTopology checks if the two topologies have
// the same label values (labels are specified in SyncReplicaElectionConstraints.NodeLabelsAntiAffinity)
func (topologyLabels PodTopologyLabels) matchesTopology(instanceTopology PodTopologyLabels) bool {
	log.Debug("matching topology", "main", topologyLabels, "second", instanceTopology)
	for mainLabelName, mainLabelValue := range topologyLabels {
		if mainLabelValue != instanceTopology[mainLabelName] {
			return false
		}
	}
	return true
}

// PodName is the name of a Pod
type PodName string

// Topology contains the cluster topology
type Topology struct {
	// Instances contains the pod topology of the instances
	// +optional
	Instances map[PodName]PodTopologyLabels `json:"instances,omitempty"`

	// NodesUsed represents the count of distinct nodes accommodating the instances.
	// A value of '1' suggests that all instances are hosted on a single node,
	// implying the absence of High Availability (HA). Ideally, this value should
	// be the same as the number of instances in the Postgres HA cluster, implying
	// shared nothing architecture on the compute side.
	// +optional
	NodesUsed int32 `json:"nodesUsed,omitempty"`

	// SuccessfullyExtracted indicates if the topology data was extract. It is useful to enact fallback behaviors
	// in synchronous replica election in case of failures
	// +optional
	SuccessfullyExtracted bool `json:"successfullyExtracted,omitempty"`
}

// RoleStatus represents the status of a managed role in the cluster
type RoleStatus string

const (
	// RoleStatusReconciled indicates the role in DB matches the Spec
	RoleStatusReconciled RoleStatus = "reconciled"
	// RoleStatusNotManaged indicates the role is not in the Spec, therefore not managed
	RoleStatusNotManaged RoleStatus = "not-managed"
	// RoleStatusPendingReconciliation indicates the role in Spec requires updated/creation in DB
	RoleStatusPendingReconciliation RoleStatus = "pending-reconciliation"
	// RoleStatusReserved indicates this is one of the roles reserved by the operator. E.g. `postgres`
	RoleStatusReserved RoleStatus = "reserved"
)

// PasswordState represents the state of the password of a managed RoleConfiguration
type PasswordState struct {
	// the last transaction ID to affect the role definition in PostgreSQL
	// +optional
	TransactionID int64 `json:"transactionID,omitempty"`
	// the resource version of the password secret
	// +optional
	SecretResourceVersion string `json:"resourceVersion,omitempty"`
}

// ManagedRoles tracks the status of a cluster's managed roles
type ManagedRoles struct {
	// ByStatus gives the list of roles in each state
	// +optional
	ByStatus map[RoleStatus][]string `json:"byStatus,omitempty"`

	// CannotReconcile lists roles that cannot be reconciled in PostgreSQL,
	// with an explanation of the cause
	// +optional
	CannotReconcile map[string][]string `json:"cannotReconcile,omitempty"`

	// PasswordStatus gives the last transaction id and password secret version for each managed role
	// +optional
	PasswordStatus map[string]PasswordState `json:"passwordStatus,omitempty"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// The total number of PVC Groups detected in the cluster. It may differ from the number of existing instance pods.
	// +optional
	Instances int `json:"instances,omitempty"`

	// The total number of ready instances in the cluster. It is equal to the number of ready instance pods.
	// +optional
	ReadyInstances int `json:"readyInstances,omitempty"`

	// InstancesStatus indicates in which status the instances are
	// +optional
	InstancesStatus map[utils.PodStatus][]string `json:"instancesStatus,omitempty"`

	// The reported state of the instances during the last reconciliation loop
	// +optional
	InstancesReportedState map[PodName]InstanceReportedState `json:"instancesReportedState,omitempty"`

	// ManagedRolesStatus reports the state of the managed roles in the cluster
	// +optional
	ManagedRolesStatus ManagedRoles `json:"managedRolesStatus,omitempty"`

	// The timeline of the Postgres cluster
	// +optional
	TimelineID int `json:"timelineID,omitempty"`

	// Instances topology.
	// +optional
	Topology Topology `json:"topology,omitempty"`

	// ID of the latest generated node (used to avoid node name clashing)
	// +optional
	LatestGeneratedNode int `json:"latestGeneratedNode,omitempty"`

	// Current primary instance
	// +optional
	CurrentPrimary string `json:"currentPrimary,omitempty"`

	// Target primary instance, this is different from the previous one
	// during a switchover or a failover
	// +optional
	TargetPrimary string `json:"targetPrimary,omitempty"`

	// How many PVCs have been created by this cluster
	// +optional
	PVCCount int32 `json:"pvcCount,omitempty"`

	// How many Jobs have been created by this cluster
	// +optional
	JobCount int32 `json:"jobCount,omitempty"`

	// List of all the PVCs created by this cluster and still available
	// which are not attached to a Pod
	// +optional
	DanglingPVC []string `json:"danglingPVC,omitempty"`

	// List of all the PVCs that have ResizingPVC condition.
	// +optional
	ResizingPVC []string `json:"resizingPVC,omitempty"`

	// List of all the PVCs that are being initialized by this cluster
	// +optional
	InitializingPVC []string `json:"initializingPVC,omitempty"`

	// List of all the PVCs not dangling nor initializing
	// +optional
	HealthyPVC []string `json:"healthyPVC,omitempty"`

	// List of all the PVCs that are unusable because another PVC is missing
	// +optional
	UnusablePVC []string `json:"unusablePVC,omitempty"`

	// Current write pod
	// +optional
	WriteService string `json:"writeService,omitempty"`

	// Current list of read pods
	// +optional
	ReadService string `json:"readService,omitempty"`

	// Current phase of the cluster
	// +optional
	Phase string `json:"phase,omitempty"`

	// Reason for the current phase
	// +optional
	PhaseReason string `json:"phaseReason,omitempty"`

	// The list of resource versions of the secrets
	// managed by the operator. Every change here is done in the
	// interest of the instance manager, which will refresh the
	// secret data
	// +optional
	SecretsResourceVersion SecretsResourceVersion `json:"secretsResourceVersion,omitempty"`

	// The list of resource versions of the configmaps,
	// managed by the operator. Every change here is done in the
	// interest of the instance manager, which will refresh the
	// configmap data
	// +optional
	ConfigMapResourceVersion ConfigMapResourceVersion `json:"configMapResourceVersion,omitempty"`

	// The configuration for the CA and related certificates, initialized with defaults.
	// +optional
	Certificates CertificatesStatus `json:"certificates,omitempty"`

	// The first recoverability point, stored as a date in RFC3339 format
	// +optional
	FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint,omitempty"`

	// Stored as a date in RFC3339 format
	// +optional
	LastSuccessfulBackup string `json:"lastSuccessfulBackup,omitempty"`

	// Stored as a date in RFC3339 format
	// +optional
	LastFailedBackup string `json:"lastFailedBackup,omitempty"`

	// The commit hash number of which this operator running
	// +optional
	CommitHash string `json:"cloudNativePGCommitHash,omitempty"`

	// The timestamp when the last actual promotion to primary has occurred
	// +optional
	CurrentPrimaryTimestamp string `json:"currentPrimaryTimestamp,omitempty"`

	// The timestamp when the primary was detected to be unhealthy
	// This field is reported when spec.failoverDelay is populated or during online upgrades
	// +optional
	CurrentPrimaryFailingSinceTimestamp string `json:"currentPrimaryFailingSinceTimestamp,omitempty"`

	// The timestamp when the last request for a new primary has occurred
	// +optional
	TargetPrimaryTimestamp string `json:"targetPrimaryTimestamp,omitempty"`

	// The integration needed by poolers referencing the cluster
	// +optional
	PoolerIntegrations *PoolerIntegrations `json:"poolerIntegrations,omitempty"`

	// The hash of the binary of the operator
	// +optional
	OperatorHash string `json:"cloudNativePGOperatorHash,omitempty"`

	// Conditions for cluster object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// List of instance names in the cluster
	// +optional
	InstanceNames []string `json:"instanceNames,omitempty"`

	// OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster
	// +optional
	OnlineUpdateEnabled bool `json:"onlineUpdateEnabled,omitempty"`

	// AzurePVCUpdateEnabled shows if the PVC online upgrade is enabled for this cluster
	// +optional
	AzurePVCUpdateEnabled bool `json:"azurePVCUpdateEnabled,omitempty"`
}

// InstanceReportedState describes the last reported state of an instance during a reconciliation loop
type InstanceReportedState struct {
	// indicates if an instance is the primary one
	IsPrimary bool `json:"isPrimary"`
	// indicates on which TimelineId the instance is
	// +optional
	TimeLineID int `json:"timeLineID,omitempty"`
}

// ClusterConditionType defines types of cluster conditions
type ClusterConditionType string

// These are valid conditions of a Cluster, some of the conditions could be owned by
// Instance Manager and some of them could be owned by reconciler.
const (
	// ConditionContinuousArchiving represents whether WAL archiving is working
	ConditionContinuousArchiving ClusterConditionType = "ContinuousArchiving"
	// ConditionBackup represents the last backup's status
	ConditionBackup ClusterConditionType = "LastBackupSucceeded"
	// ConditionClusterReady represents whether a cluster is Ready
	ConditionClusterReady ClusterConditionType = "Ready"
)

// A Condition that can be used to communicate the Backup progress
var (
	// BackupSucceededCondition is added to a backup
	// when it was completed correctly
	BackupSucceededCondition = &metav1.Condition{
		Type:    string(ConditionBackup),
		Status:  metav1.ConditionTrue,
		Reason:  string(ConditionReasonLastBackupSucceeded),
		Message: "Backup was successful",
	}

	// BackupStartingCondition is added to a backup
	// when it started
	BackupStartingCondition = &metav1.Condition{
		Type:    string(ConditionBackup),
		Status:  metav1.ConditionFalse,
		Reason:  string(ConditionBackupStarted),
		Message: "New Backup starting up",
	}

	// BuildClusterBackupFailedCondition builds
	// ConditionReasonLastBackupFailed condition
	BuildClusterBackupFailedCondition = func(err error) *metav1.Condition {
		return &metav1.Condition{
			Type:    string(ConditionBackup),
			Status:  metav1.ConditionFalse,
			Reason:  string(ConditionReasonLastBackupFailed),
			Message: err.Error(),
		}
	}
)

// ConditionStatus defines conditions of resources
type ConditionStatus string

// These are valid condition statuses. "ConditionTrue" means a resource is in the condition;
// "ConditionFalse" means a resource is not in the condition; "ConditionUnknown" means kubernetes
// can't decide if a resource is in the condition or not. In the future, we could add other
// intermediate conditions, e.g. ConditionDegraded
const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// ConditionReason defines the reason why a certain
// condition changed
type ConditionReason string

const (
	// ConditionBackupStarted means that the condition changed because the debug
	// started
	ConditionBackupStarted ConditionReason = "BackupStarted"

	// ConditionReasonLastBackupSucceeded means that the condition changed because the last backup
	// has been taken successfully
	ConditionReasonLastBackupSucceeded ConditionReason = "LastBackupSucceeded"

	// ConditionReasonLastBackupFailed means that the condition changed because the last backup
	// failed
	ConditionReasonLastBackupFailed ConditionReason = "LastBackupFailed"

	// ConditionReasonContinuousArchivingSuccess means that the condition changed because the
	// WAL archiving was working correctly
	ConditionReasonContinuousArchivingSuccess ConditionReason = "ContinuousArchivingSuccess"

	// ConditionReasonContinuousArchivingFailing means that the condition has changed because
	// the WAL archiving is not working correctly
	ConditionReasonContinuousArchivingFailing ConditionReason = "ContinuousArchivingFailing"

	// ClusterReady means that the condition changed because the cluster is ready and working properly
	ClusterReady ConditionReason = "ClusterIsReady"

	// ClusterIsNotReady means that the condition changed because the cluster is not ready
	ClusterIsNotReady ConditionReason = "ClusterIsNotReady"

	// DetachedVolume is the reason that is set when we do a rolling upgrade to add a PVC volume to a cluster
	DetachedVolume ConditionReason = "DetachedVolume"
)

// EmbeddedObjectMetadata contains metadata to be inherited by all resources related to a Cluster
type EmbeddedObjectMetadata struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PoolerIntegrations encapsulates the needed integration for the poolers referencing the cluster
type PoolerIntegrations struct {
	// +optional
	PgBouncerIntegration PgBouncerIntegrationStatus `json:"pgBouncerIntegration,omitempty"`
}

// PgBouncerIntegrationStatus encapsulates the needed integration for the pgbouncer poolers referencing the cluster
type PgBouncerIntegrationStatus struct {
	// +optional
	Secrets []string `json:"secrets,omitempty"`
}

// ReplicaClusterConfiguration encapsulates the configuration of a replica
// cluster
type ReplicaClusterConfiguration struct {
	// The name of the external cluster which is the replication origin
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source"`

	// If replica mode is enabled, this cluster will be a replica of an
	// existing cluster. Replica cluster can be created from a recovery
	// object store or via streaming through pg_basebackup.
	// Refer to the Replica clusters page of the documentation for more information.
	Enabled bool `json:"enabled"`
}

// DefaultReplicationSlotsUpdateInterval is the default in seconds for the replication slots update interval
const DefaultReplicationSlotsUpdateInterval = 30

// DefaultReplicationSlotsHASlotPrefix is the default prefix for names of replication slots used for HA.
const DefaultReplicationSlotsHASlotPrefix = "_cnpg_"

// ReplicationSlotsConfiguration encapsulates the configuration
// of replication slots
type ReplicationSlotsConfiguration struct {
	// Replication slots for high availability configuration
	// +kubebuilder:default:={"enabled": true}
	// +optional
	HighAvailability *ReplicationSlotsHAConfiguration `json:"highAvailability,omitempty"`

	// Standby will update the status of the local replication slots
	// every `updateInterval` seconds (default 30).
	// +kubebuilder:default:=30
	// +kubebuilder:validation:Minimum=1
	// +optional
	UpdateInterval int `json:"updateInterval,omitempty"`
}

// GetUpdateInterval returns the update interval, defaulting to DefaultReplicationSlotsUpdateInterval if empty
func (r *ReplicationSlotsConfiguration) GetUpdateInterval() time.Duration {
	if r == nil || r.UpdateInterval <= 0 {
		return DefaultReplicationSlotsUpdateInterval
	}
	return time.Duration(r.UpdateInterval) * time.Second
}

// ReplicationSlotsHAConfiguration encapsulates the configuration
// of the replication slots that are automatically managed by
// the operator to control the streaming replication connections
// with the standby instances for high availability (HA) purposes.
// Replication slots are a PostgreSQL feature that makes sure
// that PostgreSQL automatically keeps WAL files in the primary
// when a streaming client (in this specific case a replica that
// is part of the HA cluster) gets disconnected.
type ReplicationSlotsHAConfiguration struct {
	// If enabled (default), the operator will automatically manage replication slots
	// on the primary instance and use them in streaming replication
	// connections with all the standby instances that are part of the HA
	// cluster. If disabled, the operator will not take advantage
	// of replication slots in streaming connections with the replicas.
	// This feature also controls replication slots in replica cluster,
	// from the designated primary to its cascading replicas.
	// +optional
	// +kubebuilder:default:=true
	Enabled *bool `json:"enabled,omitempty"`

	// Prefix for replication slots managed by the operator for HA.
	// It may only contain lower case letters, numbers, and the underscore character.
	// This can only be set at creation time. By default set to `_cnpg_`.
	// +kubebuilder:default:=_cnpg_
	// +kubebuilder:validation:Pattern=^[0-9a-z_]*$
	// +optional
	SlotPrefix string `json:"slotPrefix,omitempty"`
}

// GetSlotPrefix returns the HA slot prefix, defaulting to DefaultReplicationSlotsHASlotPrefix if empty
func (r *ReplicationSlotsHAConfiguration) GetSlotPrefix() string {
	if r == nil || r.SlotPrefix == "" {
		return DefaultReplicationSlotsHASlotPrefix
	}
	return r.SlotPrefix
}

// GetSlotNameFromInstanceName returns the slot name, given the instance name.
// It returns an empty string if High Availability Replication Slots are disabled
func (r *ReplicationSlotsHAConfiguration) GetSlotNameFromInstanceName(instanceName string) string {
	if r == nil || !r.GetEnabled() {
		return ""
	}

	slotName := fmt.Sprintf(
		"%s%s",
		r.GetSlotPrefix(),
		instanceName,
	)
	sanitizedName := slotNameNegativeRegex.ReplaceAllString(strings.ToLower(slotName), "_")

	return sanitizedName
}

// GetEnabled returns false if replication slots are disabled, default is true
func (r *ReplicationSlotsHAConfiguration) GetEnabled() bool {
	if r != nil && r.Enabled != nil {
		return *r.Enabled
	}
	return true
}

// KubernetesUpgradeStrategy tells the operator if the user want to
// allocate more space while upgrading a k8s node which is hosting
// the PostgreSQL Pods or just wait for the node to come up
type KubernetesUpgradeStrategy string

const (
	// KubernetesUpgradeStrategyAllocateSpace means that the operator
	// should allocate more disk space to host data belonging to the
	// k8s node that is being updated
	KubernetesUpgradeStrategyAllocateSpace = "allocateSpace"

	// KubernetesUpgradeStrategyWaitForNode means that the operator
	// should just recreate stuff and wait for the upgraded node
	// to be ready
	KubernetesUpgradeStrategyWaitForNode = "waitForNode"
)

// NodeMaintenanceWindow contains information that the operator
// will use while upgrading the underlying node.
//
// This option is only useful when the chosen storage prevents the Pods
// from being freely moved across nodes.
type NodeMaintenanceWindow struct {
	// Reuse the existing PVC (wait for the node to come
	// up again) or not (recreate it elsewhere - when `instances` >1)
	// +optional
	// +kubebuilder:default:=true
	ReusePVC *bool `json:"reusePVC,omitempty"`

	// Is there a node maintenance activity in progress?
	// +optional
	// +kubebuilder:default:=false
	InProgress bool `json:"inProgress,omitempty"`
}

// PrimaryUpdateStrategy contains the strategy to follow when upgrading
// the primary server of the cluster as part of rolling updates
type PrimaryUpdateStrategy string

// PrimaryUpdateMethod contains the method to use when upgrading
// the primary server of the cluster as part of rolling updates
type PrimaryUpdateMethod string

const (
	// PrimaryUpdateStrategySupervised means that the operator need to wait for the
	// user to manually issue a switchover request before updating the primary
	// server (`supervised`)
	PrimaryUpdateStrategySupervised PrimaryUpdateStrategy = "supervised"

	// PrimaryUpdateStrategyUnsupervised means that the operator will proceed with the
	// selected PrimaryUpdateMethod to another updated replica and then automatically update
	// the primary server (`unsupervised`, default)
	PrimaryUpdateStrategyUnsupervised PrimaryUpdateStrategy = "unsupervised"

	// PrimaryUpdateMethodSwitchover means that the operator will switchover to another updated
	// replica when it needs to upgrade the primary instance
	PrimaryUpdateMethodSwitchover PrimaryUpdateMethod = "switchover"

	// PrimaryUpdateMethodRestart means that the operator will restart the primary instance in-place
	// when it needs to upgrade it
	PrimaryUpdateMethodRestart PrimaryUpdateMethod = "restart"

	// DefaultPgCtlTimeoutForPromotion is the default for the pg_ctl timeout when a promotion is performed.
	// It is greater than one year in seconds, big enough to simulate an infinite timeout
	DefaultPgCtlTimeoutForPromotion = 40000000

	// DefaultMaxSwitchoverDelay is the default for the pg_ctl timeout in seconds when a primary PostgreSQL instance
	// is gracefully shutdown during a switchover.
	// It is greater than one year in seconds, big enough to simulate an infinite timeout
	DefaultMaxSwitchoverDelay = 3600

	// DefaultStartupDelay is the default value for startupDelay, startupDelay will be used to calculate the
	// FailureThreshold of startupProbe, the formula is `FailureThreshold = ceiling(startDelay / periodSeconds)`,
	// the minimum value is 1
	DefaultStartupDelay = 3600
)

// PostgresConfiguration defines the PostgreSQL configuration
type PostgresConfiguration struct {
	// PostgreSQL configuration options (postgresql.conf)
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	// +optional
	PgHBA []string `json:"pg_hba,omitempty"`

	// Requirements to be met by sync replicas. This will affect how the "synchronous_standby_names" parameter will be
	// set up.
	// +optional
	SyncReplicaElectionConstraint SyncReplicaElectionConstraints `json:"syncReplicaElectionConstraint,omitempty"`

	// Lists of shared preload libraries to add to the default ones
	// +optional
	AdditionalLibraries []string `json:"shared_preload_libraries,omitempty"`

	// Options to specify LDAP configuration
	// +optional
	LDAP *LDAPConfig `json:"ldap,omitempty"`

	// Specifies the maximum number of seconds to wait when promoting an instance to primary.
	// Default value is 40000000, greater than one year in seconds,
	// big enough to simulate an infinite timeout
	// +optional
	PgCtlTimeoutForPromotion int32 `json:"promotionTimeout,omitempty"`
}

// BootstrapConfiguration contains information about how to create the PostgreSQL
// cluster. Only a single bootstrap method can be defined among the supported
// ones. `initdb` will be used as the bootstrap method if left
// unspecified. Refer to the Bootstrap page of the documentation for more
// information.
type BootstrapConfiguration struct {
	// Bootstrap the cluster via initdb
	// +optional
	InitDB *BootstrapInitDB `json:"initdb,omitempty"`

	// Bootstrap the cluster from a backup
	// +optional
	Recovery *BootstrapRecovery `json:"recovery,omitempty"`

	// Bootstrap the cluster taking a physical backup of another compatible
	// PostgreSQL instance
	// +optional
	PgBaseBackup *BootstrapPgBaseBackup `json:"pg_basebackup,omitempty"`
}

// LDAPScheme defines the possible schemes for LDAP
type LDAPScheme string

// These are the valid LDAP schemes
const (
	LDAPSchemeLDAP  LDAPScheme = "ldap"
	LDAPSchemeLDAPS LDAPScheme = "ldaps"
)

// LDAPConfig contains the parameters needed for LDAP authentication
type LDAPConfig struct {
	// LDAP hostname or IP address
	// +optional
	Server string `json:"server,omitempty"`
	// LDAP server port
	// +optional
	Port int `json:"port,omitempty"`

	// LDAP schema to be used, possible options are `ldap` and `ldaps`
	// +kubebuilder:validation:Enum=ldap;ldaps
	// +optional
	Scheme LDAPScheme `json:"scheme,omitempty"`

	// Bind as authentication configuration
	// +optional
	BindAsAuth *LDAPBindAsAuth `json:"bindAsAuth,omitempty"`

	// Bind+Search authentication configuration
	// +optional
	BindSearchAuth *LDAPBindSearchAuth `json:"bindSearchAuth,omitempty"`

	// Set to 'true' to enable LDAP over TLS. 'false' is default
	// +optional
	TLS bool `json:"tls,omitempty"`
}

// LDAPBindAsAuth provides the required fields to use the
// bind authentication for LDAP
type LDAPBindAsAuth struct {
	// Prefix for the bind authentication option
	// +optional
	Prefix string `json:"prefix,omitempty"`
	// Suffix for the bind authentication option
	// +optional
	Suffix string `json:"suffix,omitempty"`
}

// LDAPBindSearchAuth provides the required fields to use
// the bind+search LDAP authentication process
type LDAPBindSearchAuth struct {
	// Root DN to begin the user search
	// +optional
	BaseDN string `json:"baseDN,omitempty"`
	// DN of the user to bind to the directory
	// +optional
	BindDN string `json:"bindDN,omitempty"`
	// Secret with the password for the user to bind to the directory
	// +optional
	BindPassword *corev1.SecretKeySelector `json:"bindPassword,omitempty"`

	// Attribute to match against the username
	// +optional
	SearchAttribute string `json:"searchAttribute,omitempty"`
	// Search filter to use when doing the search+bind authentication
	// +optional
	SearchFilter string `json:"searchFilter,omitempty"`
}

// CertificatesConfiguration contains the needed configurations to handle server certificates.
type CertificatesConfiguration struct {
	// The secret containing the Server CA certificate. If not defined, a new secret will be created
	// with a self-signed CA and will be used to generate the TLS certificate ServerTLSSecret.<br />
	// <br />
	// Contains:<br />
	// <br />
	// - `ca.crt`: CA that should be used to validate the server certificate,
	// used as `sslrootcert` in client connection strings.<br />
	// - `ca.key`: key used to generate Server SSL certs, if ServerTLSSecret is provided,
	// this can be omitted.<br />
	// +optional
	ServerCASecret string `json:"serverCASecret,omitempty"`

	// The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as
	// `ssl_cert_file` and `ssl_key_file` so that clients can connect to postgres securely.
	// If not defined, ServerCASecret must provide also `ca.key` and a new secret will be
	// created using the provided CA.
	// +optional
	ServerTLSSecret string `json:"serverTLSSecret,omitempty"`

	// The secret of type kubernetes.io/tls containing the client certificate to authenticate as
	// the `streaming_replica` user.
	// If not defined, ClientCASecret must provide also `ca.key`, and a new secret will be
	// created using the provided CA.
	// +optional
	ReplicationTLSSecret string `json:"replicationTLSSecret,omitempty"`

	// The secret containing the Client CA certificate. If not defined, a new secret will be created
	// with a self-signed CA and will be used to generate all the client certificates.<br />
	// <br />
	// Contains:<br />
	// <br />
	// - `ca.crt`: CA that should be used to validate the client certificates,
	// used as `ssl_ca_file` of all the instances.<br />
	// - `ca.key`: key used to generate client certificates, if ReplicationTLSSecret is provided,
	// this can be omitted.<br />
	// +optional
	ClientCASecret string `json:"clientCASecret,omitempty"`

	// The list of the server alternative DNS names to be added to the generated server TLS certificates, when required.
	// +optional
	ServerAltDNSNames []string `json:"serverAltDNSNames,omitempty"`
}

// CertificatesStatus contains configuration certificates and related expiration dates.
type CertificatesStatus struct {
	// Needed configurations to handle server certificates, initialized with default values, if needed.
	CertificatesConfiguration `json:",inline"`

	// Expiration dates for all certificates.
	// +optional
	Expirations map[string]string `json:"expirations,omitempty"`
}

// BootstrapInitDB is the configuration of the bootstrap process when
// initdb is used
// Refer to the Bootstrap page of the documentation for more information.
type BootstrapInitDB struct {
	// Name of the database used by the application. Default: `app`.
	// +optional
	Database string `json:"database,omitempty"`

	// Name of the owner of the database in the instance to be used
	// by applications. Defaults to the value of the `database` key.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Name of the secret containing the initial credentials for the
	// owner of the user database. If empty a new secret will be
	// created from scratch
	// +optional
	Secret *LocalObjectReference `json:"secret,omitempty"`

	// The list of options that must be passed to initdb when creating the cluster.
	// Deprecated: This could lead to inconsistent configurations,
	// please use the explicit provided parameters instead.
	// If defined, explicit values will be ignored.
	// +optional
	Options []string `json:"options,omitempty"`

	// Whether the `-k` option should be passed to initdb,
	// enabling checksums on data pages (default: `false`)
	// +optional
	DataChecksums *bool `json:"dataChecksums,omitempty"`

	// The value to be passed as option `--encoding` for initdb (default:`UTF8`)
	// +optional
	Encoding string `json:"encoding,omitempty"`

	// The value to be passed as option `--lc-collate` for initdb (default:`C`)
	// +optional
	LocaleCollate string `json:"localeCollate,omitempty"`

	// The value to be passed as option `--lc-ctype` for initdb (default:`C`)
	// +optional
	LocaleCType string `json:"localeCType,omitempty"`

	// The value in megabytes (1 to 1024) to be passed to the `--wal-segsize`
	// option for initdb (default: empty, resulting in PostgreSQL default: 16MB)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1024
	// +optional
	WalSegmentSize int `json:"walSegmentSize,omitempty"`

	// List of SQL queries to be executed as a superuser immediately
	// after the cluster has been created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitSQL []string `json:"postInitSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the application
	// database right after is created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitApplicationSQL []string `json:"postInitApplicationSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the `template1`
	// after the cluster has been created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitTemplateSQL []string `json:"postInitTemplateSQL,omitempty"`

	// Bootstraps the new cluster by importing data from an existing PostgreSQL
	// instance using logical backup (`pg_dump` and `pg_restore`)
	// +optional
	Import *Import `json:"import,omitempty"`

	// PostInitApplicationSQLRefs points references to ConfigMaps or Secrets which
	// contain SQL files, the general implementation order to these references is
	// from all Secrets to all ConfigMaps, and inside Secrets or ConfigMaps,
	// the implementation order is same as the order of each array
	// (by default empty)
	// +optional
	PostInitApplicationSQLRefs *PostInitApplicationSQLRefs `json:"postInitApplicationSQLRefs,omitempty"`
}

// SnapshotType is a type of allowed import
type SnapshotType string

const (
	// MonolithSnapshotType indicates to execute the monolith clone typology
	MonolithSnapshotType SnapshotType = "monolith"

	// MicroserviceSnapshotType indicates to execute the microservice clone typology
	MicroserviceSnapshotType SnapshotType = "microservice"
)

// Import contains the configuration to init a database from a logic snapshot of an externalCluster
type Import struct {
	// The source of the import
	Source ImportSource `json:"source"`

	// The import type. Can be `microservice` or `monolith`.
	// +kubebuilder:validation:Enum=microservice;monolith
	Type SnapshotType `json:"type"`

	// The databases to import
	Databases []string `json:"databases"`

	// The roles to import
	// +optional
	Roles []string `json:"roles,omitempty"`

	// List of SQL queries to be executed as a superuser in the application
	// database right after is imported - to be used with extreme care
	// (by default empty). Only available in microservice type.
	// +optional
	PostImportApplicationSQL []string `json:"postImportApplicationSQL,omitempty"`

	// When set to true, only the `pre-data` and `post-data` sections of
	// `pg_restore` are invoked, avoiding data import. Default: `false`.
	// +kubebuilder:default:=false
	// +optional
	SchemaOnly bool `json:"schemaOnly,omitempty"`
}

// ImportSource describes the source for the logical snapshot
type ImportSource struct {
	// The name of the externalCluster used for import
	ExternalCluster string `json:"externalCluster"`
}

// PostInitApplicationSQLRefs points references to ConfigMaps or Secrets which
// contain SQL files, the general implementation order to these references is
// from all Secrets to all ConfigMaps, and inside Secrets or ConfigMaps,
// the implementation order is same as the order of each array
type PostInitApplicationSQLRefs struct {
	// SecretRefs holds a list of references to Secrets
	// +optional
	SecretRefs []SecretKeySelector `json:"secretRefs,omitempty"`

	// ConfigMapRefs holds a list of references to ConfigMaps
	// +optional
	ConfigMapRefs []ConfigMapKeySelector `json:"configMapRefs,omitempty"`
}

// BootstrapRecovery contains the configuration required to restore
// from an existing cluster using 3 methodologies: external cluster,
// volume snapshots or backup objects. Full recovery and Point-In-Time
// Recovery are supported.
// The method can be also be used to create clusters in continuous recovery
// (replica clusters), also supporting cascading replication when `instances` >
// 1. Once the cluster exits recovery, the password for the superuser
// will be changed through the provided secret.
// Refer to the Bootstrap page of the documentation for more information.
type BootstrapRecovery struct {
	// The backup object containing the physical base backup from which to
	// initiate the recovery procedure.
	// Mutually exclusive with `source` and `volumeSnapshots`.
	// +optional
	Backup *BackupSource `json:"backup,omitempty"`

	// The external cluster whose backup we will restore. This is also
	// used as the name of the folder under which the backup is stored,
	// so it must be set to the name of the source cluster
	// Mutually exclusive with `backup`.
	// +optional
	Source string `json:"source,omitempty"`

	// The static PVC data source(s) from which to initiate the
	// recovery procedure. Currently supporting `VolumeSnapshot`
	// and `PersistentVolumeClaim` resources that map an existing
	// PVC group, compatible with CloudNativePG, and taken with
	// a cold backup copy on a fenced Postgres instance (limitation
	// which will be removed in the future when online backup
	// will be implemented).
	// Mutually exclusive with `backup`.
	// +optional
	VolumeSnapshots *DataSource `json:"volumeSnapshots,omitempty"`

	// By default, the recovery process applies all the available
	// WAL files in the archive (full recovery). However, you can also
	// end the recovery as soon as a consistent state is reached or
	// recover to a point-in-time (PITR) by specifying a `RecoveryTarget` object,
	// as expected by PostgreSQL (i.e., timestamp, transaction Id, LSN, ...).
	// More info: https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET
	// +optional
	RecoveryTarget *RecoveryTarget `json:"recoveryTarget,omitempty"`

	// Name of the database used by the application. Default: `app`.
	// +optional
	Database string `json:"database,omitempty"`

	// Name of the owner of the database in the instance to be used
	// by applications. Defaults to the value of the `database` key.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Name of the secret containing the initial credentials for the
	// owner of the user database. If empty a new secret will be
	// created from scratch
	// +optional
	Secret *LocalObjectReference `json:"secret,omitempty"`
}

// DataSource contains the configuration required to bootstrap a
// PostgreSQL cluster from an existing storage
type DataSource struct {
	// Configuration of the storage of the instances
	Storage corev1.TypedLocalObjectReference `json:"storage"`

	// Configuration of the storage for PostgreSQL WAL (Write-Ahead Log)
	// +optional
	WalStorage *corev1.TypedLocalObjectReference `json:"walStorage,omitempty"`
}

// BackupSource contains the backup we need to restore from, plus some
// information that could be needed to correctly restore it.
type BackupSource struct {
	LocalObjectReference `json:",inline"`
	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive.
	// +optional
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`
}

// BootstrapPgBaseBackup contains the configuration required to take
// a physical backup of an existing PostgreSQL cluster
type BootstrapPgBaseBackup struct {
	// The name of the server of which we need to take a physical backup
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source"`

	// Name of the database used by the application. Default: `app`.
	// +optional
	Database string `json:"database,omitempty"`

	// Name of the owner of the database in the instance to be used
	// by applications. Defaults to the value of the `database` key.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Name of the secret containing the initial credentials for the
	// owner of the user database. If empty a new secret will be
	// created from scratch
	// +optional
	Secret *LocalObjectReference `json:"secret,omitempty"`
}

// RecoveryTarget allows to configure the moment where the recovery process
// will stop. All the target options except TargetTLI are mutually exclusive.
type RecoveryTarget struct {
	// The ID of the backup from which to start the recovery process.
	// If empty (default) the operator will automatically detect the backup
	// based on targetTime or targetLSN if specified. Otherwise use the
	// latest available backup in chronological order.
	// +optional
	BackupID string `json:"backupID,omitempty"`

	// The target timeline ("latest" or a positive integer)
	// +optional
	TargetTLI string `json:"targetTLI,omitempty"`

	// The target transaction ID
	// +optional
	TargetXID string `json:"targetXID,omitempty"`

	// The target name (to be previously created
	// with `pg_create_restore_point`)
	// +optional
	TargetName string `json:"targetName,omitempty"`

	// The target LSN (Log Sequence Number)
	// +optional
	TargetLSN string `json:"targetLSN,omitempty"`

	// The target time as a timestamp in the RFC3339 standard
	// +optional
	TargetTime string `json:"targetTime,omitempty"`

	// End recovery as soon as a consistent state is reached
	// +optional
	TargetImmediate *bool `json:"targetImmediate,omitempty"`

	// Set the target to be exclusive. If omitted, defaults to false, so that
	// in Postgres, `recovery_target_inclusive` will be true
	// +optional
	Exclusive *bool `json:"exclusive,omitempty"`
}

// StorageConfiguration is the configuration of the storage of the PostgreSQL instances
type StorageConfiguration struct {
	// StorageClass to use for database data (`PGDATA`). Applied after
	// evaluating the PVC template, if available.
	// If not specified, generated PVCs will be satisfied by the
	// default storage class
	// +optional
	StorageClass *string `json:"storageClass,omitempty"`

	// Size of the storage. Required if not already specified in the PVC template.
	// Changes to this field are automatically reapplied to the created PVCs.
	// Size cannot be decreased.
	// +optional
	Size string `json:"size,omitempty"`

	// Resize existent PVCs, defaults to true
	// +optional
	// +kubebuilder:default:=true
	ResizeInUseVolumes *bool `json:"resizeInUseVolumes,omitempty"`

	// Template to be used to generate the Persistent Volume Claim
	// +optional
	PersistentVolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"pvcTemplate,omitempty"`
}

// GetSizeOrNil returns the requests storage size
func (s *StorageConfiguration) GetSizeOrNil() *resource.Quantity {
	if s == nil {
		return nil
	}

	if s.Size != "" {
		quantity, err := resource.ParseQuantity(s.Size)
		if err != nil {
			return nil
		}

		return &quantity
	}

	if s.PersistentVolumeClaimTemplate != nil {
		return s.PersistentVolumeClaimTemplate.Resources.Requests.Storage()
	}

	return nil
}

// SyncReplicaElectionConstraints contains the constraints for sync replicas election.
//
// For anti-affinity parameters two instances are considered in the same location
// if all the labels values match.
//
// In future synchronous replica election restriction by name will be supported.
type SyncReplicaElectionConstraints struct {
	// A list of node labels values to extract and compare to evaluate if the pods reside in the same topology or not
	// +optional
	NodeLabelsAntiAffinity []string `json:"nodeLabelsAntiAffinity,omitempty"`

	// This flag enables the constraints for sync replicas
	Enabled bool `json:"enabled"`
}

// AffinityConfiguration contains the info we need to create the
// affinity rules for Pods
type AffinityConfiguration struct {
	// Activates anti-affinity for the pods. The operator will define pods
	// anti-affinity unless this field is explicitly set to false
	// +optional
	EnablePodAntiAffinity *bool `json:"enablePodAntiAffinity,omitempty"`

	// TopologyKey to use for anti-affinity configuration. See k8s documentation
	// for more info on that
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// NodeSelector is map of key-value pairs used to define the nodes on which
	// the pods can run.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// NodeAffinity describes node affinity scheduling rules for the pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity
	// +optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty"`

	// Tolerations is a list of Tolerations that should be set for all the pods, in order to allow them to run
	// on tainted nodes.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// PodAntiAffinityType allows the user to decide whether pod anti-affinity between cluster instance has to be
	// considered a strong requirement during scheduling or not. Allowed values are: "preferred" (default if empty) or
	// "required". Setting it to "required", could lead to instances remaining pending until new kubernetes nodes are
	// added if all the existing nodes don't match the required pod anti-affinity rule.
	// More info:
	// https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity
	// +optional
	PodAntiAffinityType string `json:"podAntiAffinityType,omitempty"`

	// AdditionalPodAntiAffinity allows to specify pod anti-affinity terms to be added to the ones generated
	// by the operator if EnablePodAntiAffinity is set to true (default) or to be used exclusively if set to false.
	// +optional
	AdditionalPodAntiAffinity *corev1.PodAntiAffinity `json:"additionalPodAntiAffinity,omitempty"`

	// AdditionalPodAffinity allows to specify pod affinity terms to be passed to all the cluster's pods.
	// +optional
	AdditionalPodAffinity *corev1.PodAffinity `json:"additionalPodAffinity,omitempty"`
}

// RollingUpdateStatus contains the information about an instance which is
// being updated
type RollingUpdateStatus struct {
	// The image which we put into the Pod
	ImageName string `json:"imageName"`

	// When the update has been started
	// +optional
	StartedAt metav1.Time `json:"startedAt,omitempty"`
}

// BackupTarget describes the preferred targets for a backup
type BackupTarget string

const (
	// BackupTargetPrimary means backups will be performed on the primary instance
	BackupTargetPrimary = BackupTarget("primary")

	// BackupTargetStandby means backups will be performed on a standby instance if available
	BackupTargetStandby = BackupTarget("prefer-standby")

	// DefaultBackupTarget is the default BackupTarget
	DefaultBackupTarget = BackupTargetStandby
)

// CompressionType encapsulates the available types of compression
type CompressionType string

const (
	// CompressionTypeNone means no compression is performed
	CompressionTypeNone = CompressionType("")

	// CompressionTypeGzip means gzip compression is performed
	CompressionTypeGzip = CompressionType("gzip")

	// CompressionTypeBzip2 means bzip2 compression is performed
	CompressionTypeBzip2 = CompressionType("bzip2")

	// CompressionTypeSnappy means snappy compression is performed
	CompressionTypeSnappy = CompressionType("snappy")
)

// EncryptionType encapsulated the available types of encryption
type EncryptionType string

const (
	// EncryptionTypeNone means just use the bucket configuration
	EncryptionTypeNone = EncryptionType("")

	// EncryptionTypeAES256 means to use AES256 encryption
	EncryptionTypeAES256 = EncryptionType("AES256")

	// EncryptionTypeNoneAWSKMS means to use aws:kms encryption
	EncryptionTypeNoneAWSKMS = EncryptionType("aws:kms")
)

// BarmanCredentials an object containing the potential credentials for each cloud provider
type BarmanCredentials struct {
	// The credentials to use to upload data to Google Cloud Storage
	// +optional
	Google *GoogleCredentials `json:"googleCredentials,omitempty"`

	// The credentials to use to upload data to S3
	// +optional
	AWS *S3Credentials `json:"s3Credentials,omitempty"`

	// The credentials to use to upload data to Azure Blob Storage
	// +optional
	Azure *AzureCredentials `json:"azureCredentials,omitempty"`
}

// ArePopulated checks if the passed set of credentials contains
// something
func (crendentials BarmanCredentials) ArePopulated() bool {
	return crendentials.Azure != nil || crendentials.AWS != nil || crendentials.Google != nil
}

// BarmanObjectStoreConfiguration contains the backup configuration
// using Barman against an S3-compatible object storage
type BarmanObjectStoreConfiguration struct {
	// The potential credentials for each cloud provider
	BarmanCredentials `json:",inline"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive
	// +optional
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data
	// +kubebuilder:validation:MinLength=1
	DestinationPath string `json:"destinationPath"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// The configuration for the backup of the WAL stream.
	// When not defined, WAL files will be stored uncompressed and may be
	// unencrypted in the object store, according to the bucket default policy.
	// +optional
	Wal *WalBackupConfiguration `json:"wal,omitempty"`

	// The configuration to be used to backup the data files
	// When not defined, base backups files will be stored uncompressed and may
	// be unencrypted in the object store, according to the bucket default
	// policy.
	// +optional
	Data *DataBackupConfiguration `json:"data,omitempty"`

	// Tags is a list of key value pairs that will be passed to the
	// Barman --tags option.
	// +optional
	Tags map[string]string `json:"tags,omitempty"`

	// HistoryTags is a list of key value pairs that will be passed to the
	// Barman --history-tags option.
	// +optional
	HistoryTags map[string]string `json:"historyTags,omitempty"`
}

// BackupConfiguration defines how the backup of the cluster are taken.
// The supported backup methods are BarmanObjectStore and VolumeSnapshot.
// For details and examples refer to the Backup and Recovery section of the
// documentation
type BackupConfiguration struct {
	// VolumeSnapshot provides the configuration for the execution of volume snapshot backups.
	// +optional
	VolumeSnapshot *VolumeSnapshotConfiguration `json:"volumeSnapshot,omitempty"`

	// The configuration for the barman-cloud tool suite
	// +optional
	BarmanObjectStore *BarmanObjectStoreConfiguration `json:"barmanObjectStore,omitempty"`

	// RetentionPolicy is the retention policy to be used for backups
	// and WALs (i.e. '60d'). The retention policy is expressed in the form
	// of `XXu` where `XX` is a positive integer and `u` is in `[dwm]` -
	// days, weeks, months.
	// It's currently only applicable when using the BarmanObjectStore method.
	// +kubebuilder:validation:Pattern=^[1-9][0-9]*[dwm]$
	// +optional
	RetentionPolicy string `json:"retentionPolicy,omitempty"`

	// The policy to decide which instance should perform backups. Available
	// options are empty string, which will default to `prefer-standby` policy,
	// `primary` to have backups run always on primary instances, `prefer-standby`
	// to have backups run preferably on the most updated standby, if available.
	// +kubebuilder:validation:Enum=primary;prefer-standby
	// +kubebuilder:default:=prefer-standby
	// +optional
	Target BackupTarget `json:"target,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Compress a WAL file before sending it to the object store. Available
	// options are empty string (no compression, default), `gzip`, `bzip2` or `snappy`.
	// +kubebuilder:validation:Enum=gzip;bzip2;snappy
	// +optional
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	// +optional
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Number of WAL files to be either archived in parallel (when the
	// PostgreSQL instance is archiving to a backup object store) or
	// restored in parallel (when a PostgreSQL standby is fetching WAL
	// files from a recovery object store). If not specified, WAL files
	// will be processed one at a time. It accepts a positive integer as a
	// value - with 1 being the minimum accepted value.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxParallel int `json:"maxParallel,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// Compress a backup file (a tar file per tablespace) while streaming it
	// to the object store. Available options are empty string (no
	// compression, default), `gzip`, `bzip2` or `snappy`.
	// +kubebuilder:validation:Enum=gzip;bzip2;snappy
	// +optional
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	// +optional
	Encryption EncryptionType `json:"encryption,omitempty"`

	// The number of parallel jobs to be used to upload the backup, defaults
	// to 2
	// +kubebuilder:validation:Minimum=1
	// +optional
	Jobs *int32 `json:"jobs,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	// +optional
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`
}

// S3Credentials is the type for the credentials to be used to upload
// files to S3. It can be provided in two alternative ways:
//
// - explicitly passing accessKeyId and secretAccessKey
//
// - inheriting the role from the pod environment by setting inheritFromIAMRole to true
type S3Credentials struct {
	// The reference to the access key id
	// +optional
	AccessKeyIDReference *SecretKeySelector `json:"accessKeyId,omitempty"`

	// The reference to the secret access key
	// +optional
	SecretAccessKeyReference *SecretKeySelector `json:"secretAccessKey,omitempty"`

	// The reference to the secret containing the region name
	// +optional
	RegionReference *SecretKeySelector `json:"region,omitempty"`

	// The references to the session key
	// +optional
	SessionToken *SecretKeySelector `json:"sessionToken,omitempty"`

	// Use the role based authentication without providing explicitly the keys.
	// +optional
	InheritFromIAMRole bool `json:"inheritFromIAMRole,omitempty"`
}

// AzureCredentials is the type for the credentials to be used to upload
// files to Azure Blob Storage. The connection string contains every needed
// information. If the connection string is not specified, we'll need the
// storage account name and also one (and only one) of:
//
// - storageKey
// - storageSasToken
//
// - inheriting the credentials from the pod environment by setting inheritFromAzureAD to true
type AzureCredentials struct {
	// The connection string to be used
	// +optional
	ConnectionString *SecretKeySelector `json:"connectionString,omitempty"`

	// The storage account where to upload data
	// +optional
	StorageAccount *SecretKeySelector `json:"storageAccount,omitempty"`

	// The storage account key to be used in conjunction
	// with the storage account name
	// +optional
	StorageKey *SecretKeySelector `json:"storageKey,omitempty"`

	// A shared-access-signature to be used in conjunction with
	// the storage account name
	// +optional
	StorageSasToken *SecretKeySelector `json:"storageSasToken,omitempty"`

	// Use the Azure AD based authentication without providing explicitly the keys.
	// +optional
	InheritFromAzureAD bool `json:"inheritFromAzureAD,omitempty"`
}

// GoogleCredentials is the type for the Google Cloud Storage credentials.
// This needs to be specified even if we run inside a GKE environment.
type GoogleCredentials struct {
	// The secret containing the Google Cloud Storage JSON file with the credentials
	// +optional
	ApplicationCredentials *SecretKeySelector `json:"applicationCredentials,omitempty"`

	// If set to true, will presume that it's running inside a GKE environment,
	// default to false.
	// +optional
	GKEEnvironment bool `json:"gkeEnvironment,omitempty"`
}

// MonitoringConfiguration is the type containing all the monitoring
// configuration for a certain cluster
type MonitoringConfiguration struct {
	// Whether the default queries should be injected.
	// Set it to `true` if you don't want to inject default queries into the cluster.
	// Default: false.
	// +kubebuilder:default:=false
	// +optional
	DisableDefaultQueries *bool `json:"disableDefaultQueries,omitempty"`

	// The list of config maps containing the custom queries
	// +optional
	CustomQueriesConfigMap []ConfigMapKeySelector `json:"customQueriesConfigMap,omitempty"`

	// The list of secrets containing the custom queries
	// +optional
	CustomQueriesSecret []SecretKeySelector `json:"customQueriesSecret,omitempty"`

	// Enable or disable the `PodMonitor`
	// +kubebuilder:default:=false
	// +optional
	EnablePodMonitor bool `json:"enablePodMonitor,omitempty"`
}

// AreDefaultQueriesDisabled checks whether default monitoring queries should be disabled
func (m *MonitoringConfiguration) AreDefaultQueriesDisabled() bool {
	return m != nil && m.DisableDefaultQueries != nil && *m.DisableDefaultQueries
}

// ExternalCluster represents the connection parameters to an
// external cluster which is used in the other sections of the configuration
type ExternalCluster struct {
	// The server name, required
	Name string `json:"name"`

	// The list of connection parameters, such as dbname, host, username, etc
	// +optional
	ConnectionParameters map[string]string `json:"connectionParameters,omitempty"`

	// The reference to an SSL certificate to be used to connect to this
	// instance
	// +optional
	SSLCert *corev1.SecretKeySelector `json:"sslCert,omitempty"`

	// The reference to an SSL private key to be used to connect to this
	// instance
	// +optional
	SSLKey *corev1.SecretKeySelector `json:"sslKey,omitempty"`

	// The reference to an SSL CA public key to be used to connect to this
	// instance
	// +optional
	SSLRootCert *corev1.SecretKeySelector `json:"sslRootCert,omitempty"`

	// The reference to the password to be used to connect to the server
	// +optional
	Password *corev1.SecretKeySelector `json:"password,omitempty"`

	// The configuration for the barman-cloud tool suite
	// +optional
	BarmanObjectStore *BarmanObjectStoreConfiguration `json:"barmanObjectStore,omitempty"`
}

// GetServerName returns the server name, defaulting to the name of the external cluster or using the one specified
// in the BarmanObjectStore
func (in ExternalCluster) GetServerName() string {
	if in.BarmanObjectStore != nil && in.BarmanObjectStore.ServerName != "" {
		return in.BarmanObjectStore.ServerName
	}
	return in.Name
}

// EnsureOption represents whether we should enforce the presence or absence of
// a Role in a PostgreSQL instance
type EnsureOption string

// values taken by EnsureOption
const (
	EnsurePresent EnsureOption = "present"
	EnsureAbsent  EnsureOption = "absent"
)

// ManagedConfiguration represents the portions of PostgreSQL that are managed
// by the instance manager
type ManagedConfiguration struct {
	// Database roles managed by the `Cluster`
	// +optional
	Roles []RoleConfiguration `json:"roles,omitempty"`
}

// RoleConfiguration is the representation, in Kubernetes, of a PostgreSQL role
// with the additional field Ensure specifying whether to ensure the presence or
// absence of the role in the database
//
// The defaults of the CREATE ROLE command are applied
// Reference: https://www.postgresql.org/docs/current/sql-createrole.html
type RoleConfiguration struct {
	// Name of the role
	Name string `json:"name"`
	// Description of the role
	// +optional
	Comment string `json:"comment,omitempty"`

	// Ensure the role is `present` or `absent` - defaults to "present"
	// +kubebuilder:default:="present"
	// +kubebuilder:validation:Enum=present;absent
	// +optional
	Ensure EnsureOption `json:"ensure,omitempty"`

	// Secret containing the password of the role (if present)
	// If null, the password will be ignored unless DisablePassword is set
	// +optional
	PasswordSecret *LocalObjectReference `json:"passwordSecret,omitempty"`

	// If the role can log in, this specifies how many concurrent
	// connections the role can make. `-1` (the default) means no limit.
	// +kubebuilder:default:=-1
	// +optional
	ConnectionLimit int64 `json:"connectionLimit,omitempty"`

	// Date and time after which the role's password is no longer valid.
	// When omitted, the password will never expire (default).
	// +optional
	ValidUntil *metav1.Time `json:"validUntil,omitempty"`

	// List of one or more existing roles to which this role will be
	// immediately added as a new member. Default empty.
	// +optional
	InRoles []string `json:"inRoles,omitempty"`

	// Whether a role "inherits" the privileges of roles it is a member of.
	// Defaults is `true`.
	// +kubebuilder:default:=true
	// +optional
	Inherit *bool `json:"inherit,omitempty"` // IMPORTANT default is INHERIT

	// DisablePassword indicates that a role's password should be set to NULL in Postgres
	// +optional
	DisablePassword bool `json:"disablePassword,omitempty"`

	// Whether the role is a `superuser` who can override all access
	// restrictions within the database - superuser status is dangerous and
	// should be used only when really needed. You must yourself be a
	// superuser to create a new superuser. Defaults is `false`.
	// +optional
	Superuser bool `json:"superuser,omitempty"`

	// When set to `true`, the role being defined will be allowed to create
	// new databases. Specifying `false` (default) will deny a role the
	// ability to create databases.
	// +optional
	CreateDB bool `json:"createdb,omitempty"`

	// Whether the role will be permitted to create, alter, drop, comment
	// on, change the security label for, and grant or revoke membership in
	// other roles. Default is `false`.
	// +optional
	CreateRole bool `json:"createrole,omitempty"`

	// Whether the role is allowed to log in. A role having the `login`
	// attribute can be thought of as a user. Roles without this attribute
	// are useful for managing database privileges, but are not users in
	// the usual sense of the word. Default is `false`.
	// +optional
	Login bool `json:"login,omitempty"`

	// Whether a role is a replication role. A role must have this
	// attribute (or be a superuser) in order to be able to connect to the
	// server in replication mode (physical or logical replication) and in
	// order to be able to create or drop replication slots. A role having
	// the `replication` attribute is a very highly privileged role, and
	// should only be used on roles actually used for replication. Default
	// is `false`.
	// +optional
	Replication bool `json:"replication,omitempty"`

	// Whether a role bypasses every row-level security (RLS) policy.
	// Default is `false`.
	// +optional
	BypassRLS bool `json:"bypassrls,omitempty"` // Row-Level Security
}

// GetRoleSecretsName gets the name of the secret which is used to store the role's password
func (roleConfiguration *RoleConfiguration) GetRoleSecretsName() string {
	if roleConfiguration.PasswordSecret != nil {
		return roleConfiguration.PasswordSecret.Name
	}
	return ""
}

// GetRoleInherit return the inherit attribute of a roleConfiguration
func (roleConfiguration *RoleConfiguration) GetRoleInherit() bool {
	if roleConfiguration.Inherit != nil {
		return *roleConfiguration.Inherit
	}
	return true
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.instances,statuspath=.status.instances
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Instances",type="integer",JSONPath=".status.instances",description="Number of instances"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyInstances",description="Number of ready instances"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="Cluster current status"
// +kubebuilder:printcolumn:name="Primary",type="string",JSONPath=".status.currentPrimary",description="Primary pod"

// Cluster is the Schema for the PostgreSQL API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired behavior of the cluster.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec ClusterSpec `json:"spec"`
	// Most recently observed status of the cluster. This data may not be up
	// to date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of clusters
	Items []Cluster `json:"items"`
}

// SecretsResourceVersion is the resource versions of the secrets
// managed by the operator
type SecretsResourceVersion struct {
	// The resource version of the "postgres" user secret
	// +optional
	SuperuserSecretVersion string `json:"superuserSecretVersion,omitempty"`

	// The resource version of the "streaming_replica" user secret
	// +optional
	ReplicationSecretVersion string `json:"replicationSecretVersion,omitempty"`

	// The resource version of the "app" user secret
	// +optional
	ApplicationSecretVersion string `json:"applicationSecretVersion,omitempty"`

	// The resource versions of the managed roles secrets
	// +optional
	ManagedRoleSecretVersions map[string]string `json:"managedRoleSecretVersion,omitempty"`

	// Unused. Retained for compatibility with old versions.
	// +optional
	CASecretVersion string `json:"caSecretVersion,omitempty"`

	// The resource version of the PostgreSQL client-side CA secret version
	// +optional
	ClientCASecretVersion string `json:"clientCaSecretVersion,omitempty"`

	// The resource version of the PostgreSQL server-side CA secret version
	// +optional
	ServerCASecretVersion string `json:"serverCaSecretVersion,omitempty"`

	// The resource version of the PostgreSQL server-side secret version
	// +optional
	ServerSecretVersion string `json:"serverSecretVersion,omitempty"`

	// The resource version of the Barman Endpoint CA if provided
	// +optional
	BarmanEndpointCA string `json:"barmanEndpointCA,omitempty"`

	// A map with the versions of all the secrets used to pass metrics.
	// Map keys are the secret names, map values are the versions
	// +optional
	Metrics map[string]string `json:"metrics,omitempty"`
}

// ConfigMapResourceVersion is the resource versions of the secrets
// managed by the operator
type ConfigMapResourceVersion struct {
	// A map with the versions of all the config maps used to pass metrics.
	// Map keys are the config map names, map values are the versions
	// +optional
	Metrics map[string]string `json:"metrics,omitempty"`
}

// SetManagedRoleSecretVersion Add or update or delete the resource version of the managed role secret
func (secretResourceVersion *SecretsResourceVersion) SetManagedRoleSecretVersion(secret string, version *string) {
	if secretResourceVersion.ManagedRoleSecretVersions == nil {
		secretResourceVersion.ManagedRoleSecretVersions = make(map[string]string)
	}
	if version == nil {
		delete(secretResourceVersion.ManagedRoleSecretVersions, secret)
	} else {
		secretResourceVersion.ManagedRoleSecretVersions[secret] = *version
	}
}

// GetImageName get the name of the image that should be used
// to create the pods
func (cluster *Cluster) GetImageName() string {
	if len(cluster.Spec.ImageName) > 0 {
		return cluster.Spec.ImageName
	}

	return configuration.Current.PostgresImageName
}

// GetPostgresqlVersion gets the PostgreSQL image version detecting it from the
// image name.
// Example:
//
// ghcr.io/cloudnative-pg/postgresql:14.0 corresponds to version 140000
// ghcr.io/cloudnative-pg/postgresql:13.2 corresponds to version 130002
// ghcr.io/cloudnative-pg/postgresql:9.6.3 corresponds to version 90603
func (cluster *Cluster) GetPostgresqlVersion() (int, error) {
	image := cluster.GetImageName()
	tag := utils.GetImageTag(image)
	return postgres.GetPostgresVersionFromTag(tag)
}

// GetImagePullSecret get the name of the pull secret to use
// to download the PostgreSQL image
func (cluster *Cluster) GetImagePullSecret() string {
	return cluster.Name + ClusterSecretSuffix
}

// GetSuperuserSecretName get the secret name of the PostgreSQL superuser
func (cluster *Cluster) GetSuperuserSecretName() string {
	if cluster.Spec.SuperuserSecret != nil &&
		cluster.Spec.SuperuserSecret.Name != "" {
		return cluster.Spec.SuperuserSecret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, SuperUserSecretSuffix)
}

// GetEnableLDAPAuth return true if bind or bind+search method are
// configured in the cluster configuration
func (cluster *Cluster) GetEnableLDAPAuth() bool {
	if cluster.Spec.PostgresConfiguration.LDAP != nil &&
		(cluster.Spec.PostgresConfiguration.LDAP.BindAsAuth != nil ||
			cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth != nil) {
		return true
	}
	return false
}

// GetLDAPSecretName gets the secret name containing the LDAP password
func (cluster *Cluster) GetLDAPSecretName() string {
	if cluster.Spec.PostgresConfiguration.LDAP != nil &&
		cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth != nil &&
		cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth.BindPassword != nil {
		return cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth.BindPassword.Name
	}
	return ""
}

// ContainsManagedRolesConfiguration returns true iff there are managed roles configured
func (cluster *Cluster) ContainsManagedRolesConfiguration() bool {
	return cluster.Spec.Managed != nil && len(cluster.Spec.Managed.Roles) > 0
}

// UsesSecretInManagedRoles checks if the given secret name is used in a managed role
func (cluster *Cluster) UsesSecretInManagedRoles(secretName string) bool {
	if !cluster.ContainsManagedRolesConfiguration() {
		return false
	}
	for _, role := range cluster.Spec.Managed.Roles {
		if role.PasswordSecret != nil && role.PasswordSecret.Name == secretName {
			return true
		}
	}
	return false
}

// GetApplicationSecretName get the name of the application secret for any bootstrap type
func (cluster *Cluster) GetApplicationSecretName() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
	}
	recovery := bootstrap.Recovery
	if recovery != nil && recovery.Secret != nil && recovery.Secret.Name != "" {
		return recovery.Secret.Name
	}

	pgBaseBackup := bootstrap.PgBaseBackup
	if pgBaseBackup != nil && pgBaseBackup.Secret != nil && pgBaseBackup.Secret.Name != "" {
		return pgBaseBackup.Secret.Name
	}

	initDB := bootstrap.InitDB
	if initDB != nil && initDB.Secret != nil && initDB.Secret.Name != "" {
		return initDB.Secret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
}

// GetApplicationDatabaseName get the name of the application database for a specific bootstrap
func (cluster *Cluster) GetApplicationDatabaseName() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return ""
	}

	if bootstrap.Recovery != nil && bootstrap.Recovery.Database != "" {
		return bootstrap.Recovery.Database
	}

	if bootstrap.PgBaseBackup != nil && bootstrap.PgBaseBackup.Database != "" {
		return bootstrap.PgBaseBackup.Database
	}

	if bootstrap.InitDB != nil && bootstrap.InitDB.Database != "" {
		return bootstrap.InitDB.Database
	}

	return ""
}

// GetApplicationDatabaseOwner get the owner user of the application database for a specific bootstrap
func (cluster *Cluster) GetApplicationDatabaseOwner() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return ""
	}

	if bootstrap.Recovery != nil && bootstrap.Recovery.Owner != "" {
		return bootstrap.Recovery.Owner
	}

	if bootstrap.PgBaseBackup != nil && bootstrap.PgBaseBackup.Owner != "" {
		return bootstrap.PgBaseBackup.Owner
	}

	if bootstrap.InitDB != nil && bootstrap.InitDB.Owner != "" {
		return bootstrap.InitDB.Owner
	}

	return ""
}

// GetServerCASecretName get the name of the secret containing the CA
// of the cluster
func (cluster *Cluster) GetServerCASecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ServerCASecret != "" {
		return cluster.Spec.Certificates.ServerCASecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, DefaultServerCaSecretSuffix)
}

// GetServerTLSSecretName get the name of the secret containing the
// certificate that is used for the PostgreSQL servers
func (cluster *Cluster) GetServerTLSSecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ServerTLSSecret != "" {
		return cluster.Spec.Certificates.ServerTLSSecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ServerSecretSuffix)
}

// GetClientCASecretName get the name of the secret containing the CA
// of the cluster
func (cluster *Cluster) GetClientCASecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ClientCASecret != "" {
		return cluster.Spec.Certificates.ClientCASecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ClientCaSecretSuffix)
}

// GetFixedInheritedAnnotations gets the annotations that should be
// inherited by all resources according the cluster spec
func (cluster *Cluster) GetFixedInheritedAnnotations() map[string]string {
	if cluster.Spec.InheritedMetadata == nil || cluster.Spec.InheritedMetadata.Annotations == nil {
		return nil
	}
	return cluster.Spec.InheritedMetadata.Annotations
}

// GetFixedInheritedLabels gets the labels that should be
// inherited by all resources according the cluster spec
func (cluster *Cluster) GetFixedInheritedLabels() map[string]string {
	if cluster.Spec.InheritedMetadata == nil || cluster.Spec.InheritedMetadata.Labels == nil {
		return nil
	}
	return cluster.Spec.InheritedMetadata.Labels
}

// GetReplicationSecretName get the name of the secret for the replication user
func (cluster *Cluster) GetReplicationSecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ReplicationTLSSecret != "" {
		return cluster.Spec.Certificates.ReplicationTLSSecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ReplicationSecretSuffix)
}

// GetServiceAnyName return the name of the service that is used as DNS
// domain for all the nodes, even if they are not ready
func (cluster *Cluster) GetServiceAnyName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceAnySuffix)
}

// GetServiceReadName return the name of the service that is used for
// read transactions (including the primary)
func (cluster *Cluster) GetServiceReadName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadSuffix)
}

// GetServiceReadOnlyName return the name of the service that is used for
// read-only transactions (excluding the primary)
func (cluster *Cluster) GetServiceReadOnlyName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadOnlySuffix)
}

// GetServiceReadWriteName return the name of the service that is used for
// read-write transactions
func (cluster *Cluster) GetServiceReadWriteName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadWriteSuffix)
}

// GetMaxStartDelay get the amount of time of startDelay config option
func (cluster *Cluster) GetMaxStartDelay() int32 {
	if cluster.Spec.MaxStartDelay > 0 {
		return cluster.Spec.MaxStartDelay
	}
	return DefaultStartupDelay
}

// GetMaxStopDelay get the amount of time PostgreSQL has to stop
func (cluster *Cluster) GetMaxStopDelay() int32 {
	if cluster.Spec.MaxStopDelay > 0 {
		return cluster.Spec.MaxStopDelay
	}
	return 1800
}

// GetSmartStopDelay is used to compute the timeout of smart shutdown by the formula `stopDelay -  smartStopDelay`
func (cluster *Cluster) GetSmartStopDelay() int32 {
	if cluster.Spec.SmartStopDelay > 0 {
		return cluster.Spec.SmartStopDelay
	}
	return 180
}

// GetMaxSwitchoverDelay get the amount of time PostgreSQL has to stop before switchover
func (cluster *Cluster) GetMaxSwitchoverDelay() int32 {
	if cluster.Spec.MaxSwitchoverDelay > 0 {
		return cluster.Spec.MaxSwitchoverDelay
	}
	return DefaultMaxSwitchoverDelay
}

// GetPrimaryUpdateStrategy get the cluster primary update strategy,
// defaulting to unsupervised
func (cluster *Cluster) GetPrimaryUpdateStrategy() PrimaryUpdateStrategy {
	strategy := cluster.Spec.PrimaryUpdateStrategy
	if strategy == "" {
		return PrimaryUpdateStrategyUnsupervised
	}

	return strategy
}

// GetPrimaryUpdateMethod get the cluster primary update method,
// defaulting to restart
func (cluster *Cluster) GetPrimaryUpdateMethod() PrimaryUpdateMethod {
	strategy := cluster.Spec.PrimaryUpdateMethod
	if strategy == "" {
		return PrimaryUpdateMethodRestart
	}

	return strategy
}

// IsNodeMaintenanceWindowInProgress check if the upgrade mode is active or not
func (cluster *Cluster) IsNodeMaintenanceWindowInProgress() bool {
	return cluster.Spec.NodeMaintenanceWindow != nil && cluster.Spec.NodeMaintenanceWindow.InProgress
}

// GetPgCtlTimeoutForPromotion returns the timeout that should be waited for an instance to be promoted
// to primary. As default, DefaultPgCtlTimeoutForPromotion is big enough to simulate an infinite timeout
func (cluster *Cluster) GetPgCtlTimeoutForPromotion() int32 {
	timeout := cluster.Spec.PostgresConfiguration.PgCtlTimeoutForPromotion
	if timeout == 0 {
		return DefaultPgCtlTimeoutForPromotion
	}
	return timeout
}

// IsReusePVCEnabled check if in a maintenance window we should reuse PVCs
func (cluster *Cluster) IsReusePVCEnabled() bool {
	reusePVC := true
	if cluster.Spec.NodeMaintenanceWindow != nil && cluster.Spec.NodeMaintenanceWindow.ReusePVC != nil {
		reusePVC = *cluster.Spec.NodeMaintenanceWindow.ReusePVC
	}
	return reusePVC
}

// IsInstanceFenced check if in a given instance should be fenced
func (cluster *Cluster) IsInstanceFenced(instance string) bool {
	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		return false
	}

	if fencedInstances.Has(utils.FenceAllServers) {
		return true
	}
	return fencedInstances.Has(instance)
}

// ShouldResizeInUseVolumes is true when we should resize PVC we already
// created
func (cluster *Cluster) ShouldResizeInUseVolumes() bool {
	if cluster.Spec.StorageConfiguration.ResizeInUseVolumes == nil {
		return true
	}

	return *cluster.Spec.StorageConfiguration.ResizeInUseVolumes
}

// ShouldCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase, we need to create a secret to store application credentials
func (cluster *Cluster) ShouldCreateApplicationSecret() bool {
	return cluster.ShouldInitDBCreateApplicationSecret() ||
		cluster.ShouldPgBaseBackupCreateApplicationSecret() ||
		cluster.ShouldRecoveryCreateApplicationSecret()
}

// ShouldInitDBCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using initDB, we need to create an new application secret
func (cluster *Cluster) ShouldInitDBCreateApplicationSecret() bool {
	return cluster.ShouldInitDBCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.InitDB.Secret == nil ||
			cluster.Spec.Bootstrap.InitDB.Secret.Name == "")
}

// ShouldPgBaseBackupCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using pg_basebackup, we need to create an application secret
func (cluster *Cluster) ShouldPgBaseBackupCreateApplicationSecret() bool {
	return cluster.ShouldPgBaseBackupCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.PgBaseBackup.Secret == nil ||
			cluster.Spec.Bootstrap.PgBaseBackup.Secret.Name == "")
}

// ShouldRecoveryCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using recovery, we need to create an application secret
func (cluster *Cluster) ShouldRecoveryCreateApplicationSecret() bool {
	return cluster.ShouldRecoveryCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.Recovery.Secret == nil ||
			cluster.Spec.Bootstrap.Recovery.Secret.Name == "")
}

// ShouldCreateApplicationDatabase returns true if for this cluster,
// during the bootstrap phase, we need to create an application database
func (cluster *Cluster) ShouldCreateApplicationDatabase() bool {
	return cluster.ShouldInitDBCreateApplicationDatabase() ||
		cluster.ShouldRecoveryCreateApplicationDatabase() ||
		cluster.ShouldPgBaseBackupCreateApplicationDatabase()
}

// ShouldInitDBRunPostInitApplicationSQLRefs returns true if for this cluster,
// during the bootstrap phase using initDB, we need to run post application
// SQL files from provided references.
func (cluster *Cluster) ShouldInitDBRunPostInitApplicationSQLRefs() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs == nil {
		return false
	}

	return (len(cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs.ConfigMapRefs) != 0 ||
		len(cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs.SecretRefs) != 0)
}

// ShouldInitDBCreateApplicationDatabase returns true if the application database needs to be created during initdb
// job
func (cluster *Cluster) ShouldInitDBCreateApplicationDatabase() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	initDBParameters := cluster.Spec.Bootstrap.InitDB
	return initDBParameters.Owner != "" && initDBParameters.Database != ""
}

// ShouldPgBaseBackupCreateApplicationDatabase returns true if the application database needs to be created during the
// pg_basebackup job
func (cluster *Cluster) ShouldPgBaseBackupCreateApplicationDatabase() bool {
	// we skip creating the application database if cluster is a replica
	if cluster.IsReplica() {
		return false
	}
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.PgBaseBackup == nil {
		return false
	}

	pgBaseBackupParameters := cluster.Spec.Bootstrap.PgBaseBackup
	return pgBaseBackupParameters.Owner != "" && pgBaseBackupParameters.Database != ""
}

// ShouldRecoveryCreateApplicationDatabase returns true if the application database needs to be created during the
// recovery job
func (cluster *Cluster) ShouldRecoveryCreateApplicationDatabase() bool {
	// we skip creating the application database if cluster is a replica
	if cluster.IsReplica() {
		return false
	}

	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.Recovery == nil {
		return false
	}

	recoveryParameters := cluster.Spec.Bootstrap.Recovery
	return recoveryParameters.Owner != "" && recoveryParameters.Database != ""
}

// ShouldCreateProjectedVolume returns whether we should create the projected all in one volume
func (cluster *Cluster) ShouldCreateProjectedVolume() bool {
	return cluster.Spec.ProjectedVolumeTemplate != nil
}

// ShouldCreateWalArchiveVolume returns whether we should create the wal archive volume
func (cluster *Cluster) ShouldCreateWalArchiveVolume() bool {
	return cluster.Spec.WalStorage != nil
}

// GetPostgresUID returns the UID that is being used for the "postgres"
// user
func (cluster Cluster) GetPostgresUID() int64 {
	if cluster.Spec.PostgresUID == 0 {
		return defaultPostgresUID
	}
	return cluster.Spec.PostgresUID
}

// GetPostgresGID returns the GID that is being used for the "postgres"
// user
func (cluster Cluster) GetPostgresGID() int64 {
	if cluster.Spec.PostgresGID == 0 {
		return defaultPostgresGID
	}
	return cluster.Spec.PostgresGID
}

// ExternalCluster gets the external server with a known name, returning
// true if the server was found and false otherwise
func (cluster Cluster) ExternalCluster(name string) (ExternalCluster, bool) {
	for _, server := range cluster.Spec.ExternalClusters {
		if server.Name == name {
			return server, true
		}
	}

	return ExternalCluster{}, false
}

// IsReplica checks if this is a replica cluster or not
func (cluster Cluster) IsReplica() bool {
	return cluster.Spec.ReplicaCluster != nil && cluster.Spec.ReplicaCluster.Enabled
}

var slotNameNegativeRegex = regexp.MustCompile("[^a-z0-9_]+")

// GetSlotNameFromInstanceName returns the slot name, given the instance name.
// It returns an empty string if High Availability Replication Slots are disabled
func (cluster Cluster) GetSlotNameFromInstanceName(instanceName string) string {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil ||
		!cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled() {
		return ""
	}

	return cluster.Spec.ReplicationSlots.HighAvailability.GetSlotNameFromInstanceName(instanceName)
}

// GetBarmanEndpointCAForReplicaCluster checks if this is a replica cluster which needs barman endpoint CA
func (cluster Cluster) GetBarmanEndpointCAForReplicaCluster() *SecretKeySelector {
	if !cluster.IsReplica() {
		return nil
	}
	sourceName := cluster.Spec.ReplicaCluster.Source
	externalCluster, found := cluster.ExternalCluster(sourceName)
	if !found || externalCluster.BarmanObjectStore == nil {
		return nil
	}
	return externalCluster.BarmanObjectStore.EndpointCA
}

// GetClusterAltDNSNames returns all the names needed to build a valid Server Certificate
func (cluster *Cluster) GetClusterAltDNSNames() []string {
	defaultAltDNSNames := []string{
		cluster.GetServiceReadWriteName(),
		fmt.Sprintf("%v.%v", cluster.GetServiceReadWriteName(), cluster.Namespace),
		fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadWriteName(), cluster.Namespace),
		cluster.GetServiceReadName(),
		fmt.Sprintf("%v.%v", cluster.GetServiceReadName(), cluster.Namespace),
		fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadName(), cluster.Namespace),
		cluster.GetServiceReadOnlyName(),
		fmt.Sprintf("%v.%v", cluster.GetServiceReadOnlyName(), cluster.Namespace),
		fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadOnlyName(), cluster.Namespace),
	}

	if cluster.Spec.Certificates == nil {
		return defaultAltDNSNames
	}

	return append(defaultAltDNSNames, cluster.Spec.Certificates.ServerAltDNSNames...)
}

// UsesSecret checks whether a given secret is used by a Cluster.
//
// This function is also used to discover the set of clusters that
// should be reconciled when a certain secret changes.
func (cluster *Cluster) UsesSecret(secret string) bool {
	if _, ok := cluster.Status.SecretsResourceVersion.Metrics[secret]; ok {
		return true
	}
	certificates := cluster.Status.Certificates
	switch secret {
	case cluster.GetSuperuserSecretName(),
		cluster.GetApplicationSecretName(),
		certificates.ClientCASecret,
		certificates.ReplicationTLSSecret,
		certificates.ServerCASecret,
		certificates.ServerTLSSecret:
		return true
	}

	if cluster.UsesSecretInManagedRoles(secret) {
		return true
	}

	if cluster.Spec.Backup.IsBarmanEndpointCASet() && cluster.Spec.Backup.BarmanObjectStore.EndpointCA.Name == secret {
		return true
	}

	if endpointCA := cluster.GetBarmanEndpointCAForReplicaCluster(); endpointCA != nil && endpointCA.Name == secret {
		return true
	}

	if cluster.Status.PoolerIntegrations != nil {
		for _, pgBouncerSecretName := range cluster.Status.PoolerIntegrations.PgBouncerIntegration.Secrets {
			if pgBouncerSecretName == secret {
				return true
			}
		}
	}

	return false
}

// UsesConfigMap checks whether a given secret is used by a Cluster
func (cluster *Cluster) UsesConfigMap(config string) (ok bool) {
	if _, ok := cluster.Status.ConfigMapResourceVersion.Metrics[config]; ok {
		return true
	}
	return false
}

// IsPodMonitorEnabled checks if the PodMonitor object needs to be created
func (cluster *Cluster) IsPodMonitorEnabled() bool {
	if cluster.Spec.Monitoring != nil {
		return cluster.Spec.Monitoring.EnablePodMonitor
	}

	return false
}

// GetEnableSuperuserAccess returns if the superuser access is enabled or not
func (cluster *Cluster) GetEnableSuperuserAccess() bool {
	if cluster.Spec.EnableSuperuserAccess != nil {
		return *cluster.Spec.EnableSuperuserAccess
	}

	return true
}

// LogTimestampsWithMessage prints useful information about timestamps in stdout
func (cluster *Cluster) LogTimestampsWithMessage(ctx context.Context, logMessage string) {
	contextLogger := log.FromContext(ctx)

	currentTimestamp := utils.GetCurrentTimestamp()
	keysAndValues := []interface{}{
		"phase", cluster.Status.Phase,
		"currentTimestamp", currentTimestamp,
		"targetPrimaryTimestamp", cluster.Status.TargetPrimaryTimestamp,
		"currentPrimaryTimestamp", cluster.Status.CurrentPrimaryTimestamp,
	}

	var errs []string

	// Elapsed time since the last request of promotion (TargetPrimaryTimestamp)
	if diff, err := utils.DifferenceBetweenTimestamps(
		currentTimestamp,
		cluster.Status.TargetPrimaryTimestamp,
	); err == nil {
		keysAndValues = append(
			keysAndValues,
			"msPassedSinceTargetPrimaryTimestamp",
			diff.Milliseconds(),
		)
	} else {
		errs = append(errs, err.Error())
	}

	// Elapsed time since the last promotion (CurrentPrimaryTimestamp)
	if currentPrimaryDifference, err := utils.DifferenceBetweenTimestamps(
		currentTimestamp,
		cluster.Status.CurrentPrimaryTimestamp,
	); err == nil {
		keysAndValues = append(
			keysAndValues,
			"msPassedSinceCurrentPrimaryTimestamp",
			currentPrimaryDifference.Milliseconds(),
		)
	} else {
		errs = append(errs, err.Error())
	}

	// Difference between the last promotion and the last request of promotion
	// When positive, it is the amount of time required in the last promotion
	// of a standby to a primary. If negative, it means we have a failover/switchover
	// in progress, and the value represents the last measured uptime of the primary.
	if currentPrimaryTargetDifference, err := utils.DifferenceBetweenTimestamps(
		cluster.Status.CurrentPrimaryTimestamp,
		cluster.Status.TargetPrimaryTimestamp,
	); err == nil {
		keysAndValues = append(
			keysAndValues,
			"msDifferenceBetweenCurrentAndTargetPrimary",
			currentPrimaryTargetDifference.Milliseconds(),
		)
	} else {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		keysAndValues = append(keysAndValues, "timestampParsingErrors", errs)
	}

	contextLogger.Info(logMessage, keysAndValues...)
}

// SetInheritedDataAndOwnership sets the cluster as owner of the passed object and then
// sets all the needed annotations and labels
func (cluster *Cluster) SetInheritedDataAndOwnership(obj *metav1.ObjectMeta) {
	cluster.SetInheritedData(obj)
	utils.SetAsOwnedBy(obj, cluster.ObjectMeta, cluster.TypeMeta)
}

// SetInheritedData sets all the needed annotations and labels
func (cluster *Cluster) SetInheritedData(obj *metav1.ObjectMeta) {
	utils.InheritAnnotations(obj, cluster.Annotations, cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(obj, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.LabelClusterName(obj, cluster.GetName())
	utils.SetOperatorVersion(obj, versions.Version)
}

// ShouldForceLegacyBackup if present takes a backup without passing the name argument even on barman version 3.3.0+.
// This is needed to test both backup system in the E2E suite
func (cluster *Cluster) ShouldForceLegacyBackup() bool {
	return cluster.Annotations[utils.LegacyBackupAnnotationName] == "true"
}

// GetSeccompProfile return the proper SeccompProfile set in the cluster for Pods and Containers
func (cluster *Cluster) GetSeccompProfile() *corev1.SeccompProfile {
	if cluster.Spec.SeccompProfile != nil {
		return cluster.Spec.SeccompProfile
	}

	return &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
}

// GetCoredumpFilter get the coredump filter value from the cluster annotation
func (cluster *Cluster) GetCoredumpFilter() string {
	value, ok := cluster.Annotations[utils.CoredumpFilter]
	if ok {
		return value
	}
	return system.DefaultCoredumpFilter
}

// IsInplaceRestartPhase returns true if the cluster is in a phase that handles the Inplace restart
func (cluster *Cluster) IsInplaceRestartPhase() bool {
	return cluster.Status.Phase == PhaseInplacePrimaryRestart ||
		cluster.Status.Phase == PhaseInplaceDeletePrimaryRestart
}

// IsBarmanBackupConfigured returns true if one of the possible backup destination
// is configured, false otherwise
func (backupConfiguration *BackupConfiguration) IsBarmanBackupConfigured() bool {
	return backupConfiguration != nil && backupConfiguration.BarmanObjectStore != nil &&
		backupConfiguration.BarmanObjectStore.BarmanCredentials.ArePopulated()
}

// IsBarmanEndpointCASet returns true if we have a CA bundle for the endpoint
// false otherwise
func (backupConfiguration *BackupConfiguration) IsBarmanEndpointCASet() bool {
	return backupConfiguration != nil &&
		backupConfiguration.BarmanObjectStore != nil &&
		backupConfiguration.BarmanObjectStore.EndpointCA != nil &&
		backupConfiguration.BarmanObjectStore.EndpointCA.Name != "" &&
		backupConfiguration.BarmanObjectStore.EndpointCA.Key != ""
}

// BuildPostgresOptions create the list of options that
// should be added to the PostgreSQL configuration to
// recover given a certain target
func (target *RecoveryTarget) BuildPostgresOptions() string {
	result := ""

	if target == nil {
		return result
	}

	if target.TargetTLI != "" {
		result += fmt.Sprintf(
			"recovery_target_timeline = '%v'\n",
			target.TargetTLI)
	}
	if target.TargetXID != "" {
		result += fmt.Sprintf(
			"recovery_target_xid = '%v'\n",
			target.TargetXID)
	}
	if target.TargetName != "" {
		result += fmt.Sprintf(
			"recovery_target_name = '%v'\n",
			target.TargetName)
	}
	if target.TargetLSN != "" {
		result += fmt.Sprintf(
			"recovery_target_lsn = '%v'\n",
			target.TargetLSN)
	}
	if target.TargetTime != "" {
		result += fmt.Sprintf(
			"recovery_target_time = '%v'\n",
			utils.ConvertToPostgresFormat(target.TargetTime))
	}
	if target.TargetImmediate != nil && *target.TargetImmediate {
		result += "recovery_target = immediate\n"
	}
	if target.Exclusive != nil && *target.Exclusive {
		result += "recovery_target_inclusive = false\n"
	} else {
		result += "recovery_target_inclusive = true\n"
	}

	return result
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
