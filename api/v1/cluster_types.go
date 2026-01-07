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
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// TablespaceVolumeInfix is the infix added between the instance name
	// and tablespace name to get the name of PVC for a certain tablespace
	TablespaceVolumeInfix = "-tbs-"

	// StreamingReplicationUser is the name of the user we'll use for
	// streaming replication purposes
	StreamingReplicationUser = "streaming_replica"

	// DefaultPostgresUID is the default UID which is used by PostgreSQL
	DefaultPostgresUID = 26

	// DefaultPostgresGID is the default GID which is used by PostgreSQL
	DefaultPostgresGID = 26

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

	// MissingWALDiskSpaceExitCode is the exit code the instance manager
	// will use to signal that there's no more WAL disk space
	MissingWALDiskSpaceExitCode = 4

	// MissingWALArchivePlugin is the exit code used by the instance manager
	// to indicate that it started successfully, but the configured WAL
	// archiving plugin is not available.
	MissingWALArchivePlugin = 5

	// TimelineDivergenceExitCode is the exit code used by the instance manager
	// to indicate that a replica's timeline has diverged from the primary's
	// timeline after a failover, requiring PGDATA deletion and re-cloning.
	TimelineDivergenceExitCode = 6
)

// SnapshotOwnerReference defines the reference type for the owner of the snapshot.
// This specifies which owner the processed resources should relate to.
type SnapshotOwnerReference string

// Constants to represent the allowed types for SnapshotOwnerReference.
const (
	// SnapshotOwnerReferenceNone indicates that the snapshot does not have any owner reference.
	SnapshotOwnerReferenceNone SnapshotOwnerReference = "none"
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
	// TablespaceClassName specifies the Snapshot Class to be used for the tablespaces.
	// defaults to the PGDATA Snapshot Class, if set
	// +optional
	TablespaceClassName map[string]string `json:"tablespaceClassName,omitempty"`
	// SnapshotOwnerReference indicates the type of owner reference the snapshot should have
	// +optional
	// +kubebuilder:validation:Enum:=none;cluster;backup
	// +kubebuilder:default:=none
	SnapshotOwnerReference SnapshotOwnerReference `json:"snapshotOwnerReference,omitempty"`

	// Whether the default type of backup with volume snapshots is
	// online/hot (`true`, default) or offline/cold (`false`)
	// +optional
	// +kubebuilder:default:=true
	Online *bool `json:"online,omitempty"`

	// Configuration parameters to control the online/hot backup with volume snapshots
	// +kubebuilder:default:={waitForArchive:true,immediateCheckpoint:false}
	// +optional
	OnlineConfiguration OnlineConfiguration `json:"onlineConfiguration,omitempty"`
}

// OnlineConfiguration contains the configuration parameters for the online volume snapshot
type OnlineConfiguration struct {
	// If false, the function will return immediately after the backup is completed,
	// without waiting for WAL to be archived.
	// This behavior is only useful with backup software that independently monitors WAL archiving.
	// Otherwise, WAL required to make the backup consistent might be missing and make the backup useless.
	// By default, or when this parameter is true, pg_backup_stop will wait for WAL to be archived when archiving is
	// enabled.
	// On a standby, this means that it will wait only when archive_mode = always.
	// If write activity on the primary is low, it may be useful to run pg_switch_wal on the primary in order to trigger
	// an immediate segment switch.
	// +kubebuilder:default:=true
	// +optional
	WaitForArchive *bool `json:"waitForArchive,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	// +optional
	ImmediateCheckpoint *bool `json:"immediateCheckpoint,omitempty"`
}

// ImageCatalogRef defines the reference to a major version in an ImageCatalog
type ImageCatalogRef struct {
	// +kubebuilder:validation:XValidation:rule="self.kind == 'ImageCatalog' || self.kind == 'ClusterImageCatalog'",message="Only image catalogs are supported"
	// +kubebuilder:validation:XValidation:rule="self.apiGroup == 'postgresql.cnpg.io'",message="Only image catalogs are supported"
	corev1.TypedLocalObjectReference `json:",inline"`
	// The major version of PostgreSQL we want to use from the ImageCatalog
	Major int `json:"major"`
}

// +kubebuilder:validation:XValidation:rule="!(has(self.imageCatalogRef) && has(self.imageName))",message="imageName and imageCatalogRef are mutually exclusive"

// ClusterSpec defines the desired state of a PostgreSQL cluster managed by
// CloudNativePG.
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

	// Defines the major PostgreSQL version we want to use within an ImageCatalog
	// +optional
	ImageCatalogRef *ImageCatalogRef `json:"imageCatalogRef,omitempty"`

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

	// EphemeralVolumeSource allows the user to configure the source of ephemeral volumes.
	// +optional
	EphemeralVolumeSource *corev1.EphemeralVolumeSource `json:"ephemeralVolumeSource,omitempty"`

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
	// Make sure you reserve enough time for the operator to request a fast shutdown of Postgres
	// (that is: `stopDelay` - `smartShutdownTimeout`). Default is 180 seconds.
	// +kubebuilder:default:=180
	// +optional
	SmartShutdownTimeout *int32 `json:"smartShutdownTimeout,omitempty"`

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

	// LivenessProbeTimeout is the time (in seconds) that is allowed for a PostgreSQL instance
	// to successfully respond to the liveness probe (default 30).
	// The Liveness probe failure threshold is derived from this value using the formula:
	// ceiling(livenessProbe / 10).
	// +optional
	LivenessProbeTimeout *int32 `json:"livenessProbeTimeout,omitempty"`

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

	// EphemeralVolumesSizeLimit allows the user to set the limits for the ephemeral
	// volumes
	// +optional
	EphemeralVolumesSizeLimit *EphemeralVolumesSizeLimitConfiguration `json:"ephemeralVolumesSizeLimit,omitempty"`

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
	// it can be with a switchover (`switchover`) or in-place (`restart` - default).
	// Note: when using `switchover`, the operator will reject updates that change both
	// the image name and PostgreSQL configuration parameters simultaneously to avoid
	// configuration mismatches during the switchover process.
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

	// Override the PodSecurityContext applied to every Pod of the cluster.
	// When set, this overrides the operator's default PodSecurityContext for the cluster.
	// If omitted, the operator defaults are used.
	// This field doesn't have any effect if SecurityContextConstraints are present.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Override the SecurityContext applied to every Container in the Pod of the cluster.
	// When set, this overrides the operator's default Container SecurityContext.
	// If omitted, the operator defaults are used.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// The tablespaces configuration
	// +optional
	Tablespaces []TablespaceConfiguration `json:"tablespaces,omitempty"`

	// Manage the `PodDisruptionBudget` resources within the cluster. When
	// configured as `true` (default setting), the pod disruption budgets
	// will safeguard the primary node from being terminated. Conversely,
	// setting it to `false` will result in the absence of any
	// `PodDisruptionBudget` resource, permitting the shutdown of all nodes
	// hosting the PostgreSQL cluster. This latter configuration is
	// advisable for any PostgreSQL cluster employed for
	// development/staging purposes.
	// +kubebuilder:default:=true
	// +optional
	EnablePDB *bool `json:"enablePDB,omitempty"`

	// The plugins configuration, containing
	// any plugin to be loaded with the corresponding configuration
	// +optional
	Plugins []PluginConfiguration `json:"plugins,omitempty"`

	// The configuration of the probes to be injected
	// in the PostgreSQL Pods.
	// +optional
	Probes *ProbesConfiguration `json:"probes,omitempty"`
}

// ProbesConfiguration represent the configuration for the probes
// to be injected in the PostgreSQL Pods
type ProbesConfiguration struct {
	// The startup probe configuration
	Startup *ProbeWithStrategy `json:"startup,omitempty"`

	// The liveness probe configuration
	Liveness *LivenessProbe `json:"liveness,omitempty"`

	// The readiness probe configuration
	Readiness *ProbeWithStrategy `json:"readiness,omitempty"`
}

// ProbeWithStrategy is the configuration of the startup probe
type ProbeWithStrategy struct {
	// Probe is the standard probe configuration
	Probe `json:",inline"`

	// The probe strategy
	// +kubebuilder:validation:Enum=pg_isready;streaming;query
	// +optional
	Type ProbeStrategyType `json:"type,omitempty"`

	// Lag limit. Used only for `streaming` strategy
	// +optional
	MaximumLag *resource.Quantity `json:"maximumLag,omitempty"`
}

// ProbeStrategyType is the type of the strategy used to declare a PostgreSQL instance
// ready
type ProbeStrategyType string

const (
	// ProbeStrategyPgIsReady means that the pg_isready tool is used to determine
	// whether PostgreSQL is started up
	ProbeStrategyPgIsReady ProbeStrategyType = "pg_isready"

	// ProbeStrategyStreaming means that pg_isready is positive and the replica is
	// connected via streaming replication to the current primary and the lag is, if specified,
	// within the limit.
	ProbeStrategyStreaming ProbeStrategyType = "streaming"

	// ProbeStrategyQuery means that the server is able to connect to the superuser database
	// and able to execute a simple query like "-- ping"
	ProbeStrategyQuery ProbeStrategyType = "query"
)

// Probe describes a health check to be performed against a container to determine whether it is
// alive or ready to receive traffic.
type Probe struct {
	// Number of seconds after the container has started before liveness probes are initiated.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// Number of seconds after which the probe times out.
	// Defaults to 1 second. Minimum value is 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// How often (in seconds) to perform the probe.
	// Default to 10 seconds. Minimum value is 1.
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// Minimum consecutive successes for the probe to be considered successful after having failed.
	// Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1.
	// +optional
	SuccessThreshold int32 `json:"successThreshold,omitempty"`
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	// Defaults to 3. Minimum value is 1.
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
	// Optional duration in seconds the pod needs to terminate gracefully upon probe failure.
	// The grace period is the duration in seconds after the processes running in the pod are sent
	// a termination signal and the time when the processes are forcibly halted with a kill signal.
	// Set this value longer than the expected cleanup time for your process.
	// If this value is nil, the pod's terminationGracePeriodSeconds will be used. Otherwise, this
	// value overrides the value provided by the pod spec.
	// Value must be non-negative integer. The value zero indicates stop immediately via
	// the kill signal (no opportunity to shut down).
	// This is a beta field and requires enabling ProbeTerminationGracePeriod feature gate.
	// Minimum value is 1. spec.terminationGracePeriodSeconds is used if unset.
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// LivenessProbe is the configuration of the liveness probe
type LivenessProbe struct {
	// Probe is the standard probe configuration
	Probe `json:",inline"`

	// Configure the feature that extends the liveness probe for a primary
	// instance. In addition to the basic checks, this verifies whether the
	// primary is isolated from the Kubernetes API server and from its
	// replicas, ensuring that it can be safely shut down if network
	// partition or API unavailability is detected. Enabled by default.
	// +optional
	IsolationCheck *IsolationCheckConfiguration `json:"isolationCheck,omitempty"`
}

// IsolationCheckConfiguration contains the configuration for the isolation check
// functionality in the liveness probe
type IsolationCheckConfiguration struct {
	// Whether primary isolation checking is enabled for the liveness probe
	// +optional
	// +kubebuilder:default:=true
	Enabled *bool `json:"enabled,omitempty"`

	// Timeout in milliseconds for requests during the primary isolation check
	// +optional
	// +kubebuilder:default:=1000
	RequestTimeout int `json:"requestTimeout,omitempty"`

	// Timeout in milliseconds for connections during the primary isolation check
	// +optional
	// +kubebuilder:default:=1000
	ConnectionTimeout int `json:"connectionTimeout,omitempty"`
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

	// PhaseMajorUpgrade major version upgrade in process
	PhaseMajorUpgrade = "Upgrading Postgres major version"

	// PhaseUpgradeDelayed is set when a cluster needs to be upgraded,
	// but the operation is being delayed by the operator configuration
	PhaseUpgradeDelayed = "Cluster upgrade delayed"

	// PhaseWaitingForUser set the status to wait for an action from the user
	PhaseWaitingForUser = "Waiting for user action"

	// PhaseInplacePrimaryRestart for a cluster restarting the primary instance in-place
	PhaseInplacePrimaryRestart = "Primary instance is being restarted in-place"

	// PhaseInplaceDeletePrimaryRestart for a cluster restarting the primary instance without a switchover
	PhaseInplaceDeletePrimaryRestart = "Primary instance is being restarted without a switchover"

	// PhaseHealthy for a cluster doing nothing
	PhaseHealthy = "Cluster in healthy state"

	// PhaseUnknownPlugin is triggered when the required CNPG-i plugin have not been
	// loaded still
	PhaseUnknownPlugin = "Cluster cannot proceed to reconciliation due to an unknown plugin being required"

	// PhaseFailurePlugin is triggered when the cluster cannot proceed to reconciliation due to an interaction failure
	PhaseFailurePlugin = "Cluster cannot proceed to reconciliation due to an error while interacting with plugins"

	// PhaseImageCatalogError is triggered when the cluster cannot select the image to
	// apply because of an invalid or incomplete catalog
	PhaseImageCatalogError = "Cluster has incomplete or invalid image catalog"

	// PhaseUnrecoverable for an unrecoverable cluster
	PhaseUnrecoverable = "Cluster is unrecoverable and needs manual intervention"

	// PhaseArchitectureBinaryMissing is the error phase describing a missing architecture
	PhaseArchitectureBinaryMissing = "Cluster cannot execute instance online upgrade due to missing architecture binary"

	// PhaseWaitingForInstancesToBeActive is a waiting phase that is triggered when an instance pod is not active
	PhaseWaitingForInstancesToBeActive = "Waiting for the instances to become active"

	// PhaseOnlineUpgrading for when the instance manager is being upgraded in place
	PhaseOnlineUpgrading = "Online upgrade in progress"

	// PhaseApplyingConfiguration is set by the instance manager when a configuration
	// change is being detected
	PhaseApplyingConfiguration = "Applying configuration"

	// PhaseReplicaClusterPromotion is the phase
	PhaseReplicaClusterPromotion = "Promoting to primary cluster"

	// PhaseCannotCreateClusterObjects is set by the operator when is unable to create cluster resources
	PhaseCannotCreateClusterObjects = "Unable to create required cluster objects"
)

// EphemeralVolumesSizeLimitConfiguration contains the configuration of the ephemeral
// storage
type EphemeralVolumesSizeLimitConfiguration struct {
	// Shm is the size limit of the shared memory volume
	// +optional
	Shm *resource.Quantity `json:"shm,omitempty"`

	// TemporaryData is the size limit of the temporary data volume
	// +optional
	TemporaryData *resource.Quantity `json:"temporaryData,omitempty"`
}

// ServiceAccountTemplate contains the template needed to generate the service accounts
type ServiceAccountTemplate struct {
	// Metadata are the metadata to be used for the generated
	// service account
	Metadata Metadata `json:"metadata"`
}

// PodTopologyLabels represent the topology of a Pod. map[labelName]labelValue
type PodTopologyLabels map[string]string

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

// TablespaceState represents the state of a tablespace in a cluster
type TablespaceState struct {
	// Name is the name of the tablespace
	Name string `json:"name"`

	// Owner is the PostgreSQL user owning the tablespace
	// +optional
	Owner string `json:"owner,omitempty"`

	// State is the latest reconciliation state
	State TablespaceStatus `json:"state"`

	// Error is the reconciliation error, if any
	// +optional
	Error string `json:"error,omitempty"`
}

// TablespaceStatus represents the status of a tablespace in the cluster
type TablespaceStatus string

const (
	// TablespaceStatusReconciled indicates the tablespace in DB matches the Spec
	TablespaceStatusReconciled TablespaceStatus = "reconciled"

	// TablespaceStatusPendingReconciliation indicates the tablespace in Spec requires creation in the DB
	TablespaceStatusPendingReconciliation TablespaceStatus = "pending"
)

// AvailableArchitecture represents the state of a cluster's architecture
type AvailableArchitecture struct {
	// GoArch is the name of the executable architecture
	GoArch string `json:"goArch"`

	// Hash is the hash of the executable
	Hash string `json:"hash"`
}

// ClusterStatus defines the observed state of a PostgreSQL cluster managed by
// CloudNativePG.
type ClusterStatus struct {
	// The total number of PVC Groups detected in the cluster. It may differ from the number of existing instance pods.
	// +optional
	Instances int `json:"instances,omitempty"`

	// The total number of ready instances in the cluster. It is equal to the number of ready instance pods.
	// +optional
	ReadyInstances int `json:"readyInstances,omitempty"`

	// InstancesStatus indicates in which status the instances are
	// +optional
	InstancesStatus map[PodStatus][]string `json:"instancesStatus,omitempty"`

	// The reported state of the instances during the last reconciliation loop
	// +optional
	InstancesReportedState map[PodName]InstanceReportedState `json:"instancesReportedState,omitempty"`

	// ManagedRolesStatus reports the state of the managed roles in the cluster
	// +optional
	ManagedRolesStatus ManagedRoles `json:"managedRolesStatus,omitempty"`

	// TablespacesStatus reports the state of the declarative tablespaces in the cluster
	// +optional
	TablespacesStatus []TablespaceState `json:"tablespacesStatus,omitempty"`

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

	// LastPromotionToken is the last verified promotion token that
	// was used to promote a replica cluster
	// +optional
	LastPromotionToken string `json:"lastPromotionToken,omitempty"`

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

	// The first recoverability point, stored as a date in RFC3339 format.
	// This field is calculated from the content of FirstRecoverabilityPointByMethod.
	//
	// Deprecated: the field is not set for backup plugins.
	// +optional
	FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint,omitempty"`

	// The first recoverability point, stored as a date in RFC3339 format, per backup method type.
	//
	// Deprecated: the field is not set for backup plugins.
	// +optional
	FirstRecoverabilityPointByMethod map[BackupMethod]metav1.Time `json:"firstRecoverabilityPointByMethod,omitempty"`

	// Last successful backup, stored as a date in RFC3339 format.
	// This field is calculated from the content of LastSuccessfulBackupByMethod.
	//
	// Deprecated: the field is not set for backup plugins.
	// +optional
	LastSuccessfulBackup string `json:"lastSuccessfulBackup,omitempty"`

	// Last successful backup, stored as a date in RFC3339 format, per backup method type.
	//
	// Deprecated: the field is not set for backup plugins.
	// +optional
	LastSuccessfulBackupByMethod map[BackupMethod]metav1.Time `json:"lastSuccessfulBackupByMethod,omitempty"`

	// Last failed backup, stored as a date in RFC3339 format.
	//
	// Deprecated: the field is not set for backup plugins.
	// +optional
	LastFailedBackup string `json:"lastFailedBackup,omitempty"`

	// The commit hash number of which this operator running
	// +optional
	CommitHash string `json:"cloudNativePGCommitHash,omitempty"`

	// The timestamp when the last actual promotion to primary has occurred
	// +optional
	CurrentPrimaryTimestamp string `json:"currentPrimaryTimestamp,omitempty"`

	// The timestamp when the primary was detected to be unhealthy
	// This field is reported when `.spec.failoverDelay` is populated or during online upgrades
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

	// AvailableArchitectures reports the available architectures of a cluster
	// +optional
	AvailableArchitectures []AvailableArchitecture `json:"availableArchitectures,omitempty"`

	// Conditions for cluster object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// List of instance names in the cluster
	// +optional
	InstanceNames []string `json:"instanceNames,omitempty"`

	// OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster
	// +optional
	OnlineUpdateEnabled bool `json:"onlineUpdateEnabled,omitempty"`

	// Image contains the image name used by the pods
	// +optional
	Image string `json:"image,omitempty"`

	// PGDataImageInfo contains the details of the latest image that has run on the current data directory.
	// +optional
	PGDataImageInfo *ImageInfo `json:"pgDataImageInfo,omitempty"`

	// PluginStatus is the status of the loaded plugins
	// +optional
	PluginStatus []PluginStatus `json:"pluginStatus,omitempty"`

	// SwitchReplicaClusterStatus is the status of the switch to replica cluster
	// +optional
	SwitchReplicaClusterStatus SwitchReplicaClusterStatus `json:"switchReplicaClusterStatus,omitempty"`

	// DemotionToken is a JSON token containing the information
	// from pg_controldata such as Database system identifier, Latest checkpoint's
	// TimeLineID, Latest checkpoint's REDO location, Latest checkpoint's REDO
	// WAL file, and Time of latest checkpoint
	// +optional
	DemotionToken string `json:"demotionToken,omitempty"`

	// SystemID is the latest detected PostgreSQL SystemID
	// +optional
	SystemID string `json:"systemID,omitempty"`
}

// ImageInfo contains the information about a PostgreSQL image
type ImageInfo struct {
	// Image is the image name
	Image string `json:"image"`
	// MajorVersion is the major version of the image
	MajorVersion int `json:"majorVersion"`
}

// SwitchReplicaClusterStatus contains all the statuses regarding the switch of a cluster to a replica cluster
type SwitchReplicaClusterStatus struct {
	// InProgress indicates if there is an ongoing procedure of switching a cluster to a replica cluster.
	// +optional
	InProgress bool `json:"inProgress,omitempty"`
}

// InstanceReportedState describes the last reported state of an instance during a reconciliation loop
type InstanceReportedState struct {
	// indicates if an instance is the primary one
	IsPrimary bool `json:"isPrimary"`
	// indicates on which TimelineId the instance is
	// +optional
	TimeLineID int `json:"timeLineID,omitempty"`
	// IP address of the instance
	IP string `json:"ip,omitempty"`
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
	// ConditionConsistentSystemID is true when the all the instances of the
	// cluster report the same System ID.
	ConditionConsistentSystemID ClusterConditionType = "ConsistentSystemID"
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
	// Self defines the name of this cluster. It is used to determine if this is a primary
	// or a replica cluster, comparing it with `primary`
	// +optional
	Self string `json:"self,omitempty"`

	// Primary defines which Cluster is defined to be the primary in the distributed PostgreSQL cluster, based on the
	// topology specified in externalClusters
	// +optional
	Primary string `json:"primary,omitempty"`

	// The name of the external cluster which is the replication origin
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source"`

	// If replica mode is enabled, this cluster will be a replica of an
	// existing cluster. Replica cluster can be created from a recovery
	// object store or via streaming through pg_basebackup.
	// Refer to the Replica clusters page of the documentation for more information.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// A demotion token generated by an external cluster used to
	// check if the promotion requirements are met.
	// +optional
	PromotionToken string `json:"promotionToken,omitempty"`

	// When replica mode is enabled, this parameter allows you to replay
	// transactions only when the system time is at least the configured
	// time past the commit time. This provides an opportunity to correct
	// data loss errors. Note that when this parameter is set, a promotion
	// token cannot be used.
	// +optional
	MinApplyDelay *metav1.Duration `json:"minApplyDelay,omitempty"`
}

// DefaultReplicationSlotsUpdateInterval is the default in seconds for the replication slots update interval
const DefaultReplicationSlotsUpdateInterval = 30

// DefaultReplicationSlotsHASlotPrefix is the default prefix for names of replication slots used for HA.
const DefaultReplicationSlotsHASlotPrefix = "_cnpg_"

// SynchronizeReplicasConfiguration contains the configuration for the synchronization of user defined
// physical replication slots
type SynchronizeReplicasConfiguration struct {
	// When set to true, every replication slot that is on the primary is synchronized on each standby
	// +kubebuilder:default:=true
	Enabled *bool `json:"enabled"`

	// List of regular expression patterns to match the names of replication slots to be excluded (by default empty)
	// +optional
	ExcludePatterns []string `json:"excludePatterns,omitempty"`
}

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

	// Configures the synchronization of the user defined physical replication slots
	// +optional
	SynchronizeReplicas *SynchronizeReplicasConfiguration `json:"synchronizeReplicas,omitempty"`
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

	// When enabled, the operator automatically manages synchronization of logical
	// decoding (replication) slots across high-availability clusters.
	//
	// Requires one of the following conditions:
	// - PostgreSQL version 17 or later
	// - PostgreSQL version < 17 with pg_failover_slots extension enabled
	//
	// +optional
	SynchronizeLogicalDecoding bool `json:"synchronizeLogicalDecoding,omitempty"`
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
	// replica when it needs to upgrade the primary instance.
	// Note: when using this method, the operator will reject updates that change both
	// the image name and PostgreSQL configuration parameters simultaneously to avoid
	// configuration mismatches during the switchover process.
	PrimaryUpdateMethodSwitchover PrimaryUpdateMethod = "switchover"

	// PrimaryUpdateMethodRestart means that the operator will restart the primary instance in-place
	// when it needs to upgrade it
	PrimaryUpdateMethodRestart PrimaryUpdateMethod = "restart"

	// DefaultPgCtlTimeoutForPromotion is the default for the pg_ctl timeout when a promotion is performed.
	// It is greater than one year in seconds, big enough to simulate an infinite timeout
	DefaultPgCtlTimeoutForPromotion = 40000000

	// DefaultMaxSwitchoverDelay is the default for the pg_ctl timeout in seconds when a primary PostgreSQL instance
	// is gracefully shutdown during a switchover.
	DefaultMaxSwitchoverDelay = 3600

	// DefaultStartupDelay is the default value for startupDelay, startupDelay will be used to calculate the
	// FailureThreshold of startupProbe, the formula is `FailureThreshold = ceiling(startDelay / periodSeconds)`,
	// the minimum value is 1
	DefaultStartupDelay = 3600
)

// SynchronousReplicaConfigurationMethod configures whether to use
// quorum based replication or a priority list
type SynchronousReplicaConfigurationMethod string

const (
	// SynchronousReplicaConfigurationMethodFirst means a priority list should be used
	SynchronousReplicaConfigurationMethodFirst = SynchronousReplicaConfigurationMethod("first")

	// SynchronousReplicaConfigurationMethodAny means that quorum based replication should be used
	SynchronousReplicaConfigurationMethodAny = SynchronousReplicaConfigurationMethod("any")
)

// DataDurabilityLevel specifies how strictly to enforce synchronous replication
// when cluster instances are unavailable. Options are `required` or `preferred`.
type DataDurabilityLevel string

const (
	// DataDurabilityLevelRequired means that data durability is strictly enforced
	DataDurabilityLevelRequired DataDurabilityLevel = "required"

	// DataDurabilityLevelPreferred means that data durability is enforced
	// only when healthy replicas are available
	DataDurabilityLevelPreferred DataDurabilityLevel = "preferred"
)

// SynchronousReplicaConfiguration contains the configuration of the
// PostgreSQL synchronous replication feature.
// Important: at this moment, also `.spec.minSyncReplicas` and `.spec.maxSyncReplicas`
// need to be considered.
// +kubebuilder:validation:XValidation:rule="self.dataDurability!='preferred' || ((!has(self.standbyNamesPre) || self.standbyNamesPre.size()==0) && (!has(self.standbyNamesPost) || self.standbyNamesPost.size()==0))",message="dataDurability set to 'preferred' requires empty 'standbyNamesPre' and empty 'standbyNamesPost'"
type SynchronousReplicaConfiguration struct {
	// Method to select synchronous replication standbys from the listed
	// servers, accepting 'any' (quorum-based synchronous replication) or
	// 'first' (priority-based synchronous replication) as values.
	// +kubebuilder:validation:Enum=any;first
	Method SynchronousReplicaConfigurationMethod `json:"method"`

	// Specifies the number of synchronous standby servers that
	// transactions must wait for responses from.
	// +kubebuilder:validation:XValidation:rule="self > 0",message="The number of synchronous replicas should be greater than zero"
	Number int `json:"number"`

	// Specifies the maximum number of local cluster pods that can be
	// automatically included in the `synchronous_standby_names` option in
	// PostgreSQL.
	// +optional
	MaxStandbyNamesFromCluster *int `json:"maxStandbyNamesFromCluster,omitempty"`

	// A user-defined list of application names to be added to
	// `synchronous_standby_names` before local cluster pods (the order is
	// only useful for priority-based synchronous replication).
	// +optional
	StandbyNamesPre []string `json:"standbyNamesPre,omitempty"`

	// A user-defined list of application names to be added to
	// `synchronous_standby_names` after local cluster pods (the order is
	// only useful for priority-based synchronous replication).
	// +optional
	StandbyNamesPost []string `json:"standbyNamesPost,omitempty"`

	// If set to "required", data durability is strictly enforced. Write operations
	// with synchronous commit settings (`on`, `remote_write`, or `remote_apply`) will
	// block if there are insufficient healthy replicas, ensuring data persistence.
	// If set to "preferred", data durability is maintained when healthy replicas
	// are available, but the required number of instances will adjust dynamically
	// if replicas become unavailable. This setting relaxes strict durability enforcement
	// to allow for operational continuity. This setting is only applicable if both
	// `standbyNamesPre` and `standbyNamesPost` are unset (empty).
	// +kubebuilder:validation:Enum=required;preferred
	// +optional
	DataDurability DataDurabilityLevel `json:"dataDurability,omitempty"`

	// FailoverQuorum enables a quorum-based check before failover, improving
	// data durability and safety during failover events in CloudNativePG-managed
	// PostgreSQL clusters.
	// +optional
	FailoverQuorum bool `json:"failoverQuorum"`
}

// PostgresConfiguration defines the PostgreSQL configuration
type PostgresConfiguration struct {
	// PostgreSQL configuration options (postgresql.conf)
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`

	// Configuration of the PostgreSQL synchronous replication feature
	// +optional
	Synchronous *SynchronousReplicaConfiguration `json:"synchronous,omitempty"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	// +optional
	PgHBA []string `json:"pg_hba,omitempty"`

	// PostgreSQL User Name Maps rules (lines to be appended
	// to the pg_ident.conf file)
	// +optional
	PgIdent []string `json:"pg_ident,omitempty"`

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

	// If this parameter is true, the user will be able to invoke `ALTER SYSTEM`
	// on this CloudNativePG Cluster.
	// This should only be used for debugging and troubleshooting.
	// Defaults to false.
	// +optional
	EnableAlterSystem bool `json:"enableAlterSystem,omitempty"`

	// The configuration of the extensions to be added
	// +optional
	Extensions []ExtensionConfiguration `json:"extensions,omitempty"`
}

// ExtensionConfiguration is the configuration used to add
// PostgreSQL extensions to the Cluster.
type ExtensionConfiguration struct {
	// The name of the extension, required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// The image containing the extension, required
	// +kubebuilder:validation:XValidation:rule="has(self.reference)",message="An image reference is required"
	ImageVolumeSource corev1.ImageVolumeSource `json:"image"`

	// The list of directories inside the image which should be added to extension_control_path.
	// If not defined, defaults to "/share".
	// +optional
	ExtensionControlPath []string `json:"extension_control_path,omitempty"`

	// The list of directories inside the image which should be added to dynamic_library_path.
	// If not defined, defaults to "/lib".
	// +optional
	DynamicLibraryPath []string `json:"dynamic_library_path,omitempty"`

	// The list of directories inside the image which should be added to ld_library_path.
	// +optional
	LdLibraryPath []string `json:"ld_library_path,omitempty"`
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
// +kubebuilder:validation:XValidation:rule="!has(self.builtinLocale) || self.localeProvider == 'builtin'",message="builtinLocale is only available when localeProvider is set to `builtin`"
// +kubebuilder:validation:XValidation:rule="!has(self.icuLocale) || self.localeProvider == 'icu'",message="icuLocale is only available when localeProvider is set to `icu`"
// +kubebuilder:validation:XValidation:rule="!has(self.icuRules) || self.localeProvider == 'icu'",message="icuRules is only available when localeProvider is set to `icu`"
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
	//
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

	// Sets the default collation order and character classification in the new database.
	// +optional
	Locale string `json:"locale,omitempty"`

	// This option sets the locale provider for databases created in the new cluster.
	// Available from PostgreSQL 16.
	// +optional
	LocaleProvider string `json:"localeProvider,omitempty"`

	// Specifies the ICU locale when the ICU provider is used.
	// This option requires `localeProvider` to be set to `icu`.
	// Available from PostgreSQL 15.
	// +optional
	IcuLocale string `json:"icuLocale,omitempty"`

	// Specifies additional collation rules to customize the behavior of the default collation.
	// This option requires `localeProvider` to be set to `icu`.
	// Available from PostgreSQL 16.
	// +optional
	IcuRules string `json:"icuRules,omitempty"`

	// Specifies the locale name when the builtin provider is used.
	// This option requires `localeProvider` to be set to `builtin`.
	// Available from PostgreSQL 17.
	// +optional
	BuiltinLocale string `json:"builtinLocale,omitempty"`

	// The value in megabytes (1 to 1024) to be passed to the `--wal-segsize`
	// option for initdb (default: empty, resulting in PostgreSQL default: 16MB)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1024
	// +optional
	WalSegmentSize int `json:"walSegmentSize,omitempty"`

	// List of SQL queries to be executed as a superuser in the `postgres`
	// database right after the cluster has been created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitSQL []string `json:"postInitSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the application
	// database right after the cluster has been created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitApplicationSQL []string `json:"postInitApplicationSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the `template1`
	// database right after the cluster has been created - to be used with extreme care
	// (by default empty)
	// +optional
	PostInitTemplateSQL []string `json:"postInitTemplateSQL,omitempty"`

	// Bootstraps the new cluster by importing data from an existing PostgreSQL
	// instance using logical backup (`pg_dump` and `pg_restore`)
	// +optional
	Import *Import `json:"import,omitempty"`

	// List of references to ConfigMaps or Secrets containing SQL files
	// to be executed as a superuser in the application database right after
	// the cluster has been created. The references are processed in a specific order:
	// first, all Secrets are processed, followed by all ConfigMaps.
	// Within each group, the processing order follows the sequence specified
	// in their respective arrays.
	// (by default empty)
	// +optional
	PostInitApplicationSQLRefs *SQLRefs `json:"postInitApplicationSQLRefs,omitempty"`

	// List of references to ConfigMaps or Secrets containing SQL files
	// to be executed as a superuser in the `template1` database right after
	// the cluster has been created. The references are processed in a specific order:
	// first, all Secrets are processed, followed by all ConfigMaps.
	// Within each group, the processing order follows the sequence specified
	// in their respective arrays.
	// (by default empty)
	// +optional
	PostInitTemplateSQLRefs *SQLRefs `json:"postInitTemplateSQLRefs,omitempty"`

	// List of references to ConfigMaps or Secrets containing SQL files
	// to be executed as a superuser in the `postgres` database right after
	// the cluster has been created. The references are processed in a specific order:
	// first, all Secrets are processed, followed by all ConfigMaps.
	// Within each group, the processing order follows the sequence specified
	// in their respective arrays.
	// (by default empty)
	// +optional
	PostInitSQLRefs *SQLRefs `json:"postInitSQLRefs,omitempty"`
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
	// +optional
	SchemaOnly bool `json:"schemaOnly,omitempty"`

	// List of custom options to pass to the `pg_dump` command.
	//
	// IMPORTANT: Use with caution. The operator does not validate these options,
	// and certain flags may interfere with its intended functionality or design.
	// You are responsible for ensuring that the provided options are compatible
	// with your environment and desired behavior.
	//
	// +optional
	PgDumpExtraOptions []string `json:"pgDumpExtraOptions,omitempty"`

	// List of custom options to pass to the `pg_restore` command.
	//
	// IMPORTANT: Use with caution. The operator does not validate these options,
	// and certain flags may interfere with its intended functionality or design.
	// You are responsible for ensuring that the provided options are compatible
	// with your environment and desired behavior.
	//
	// +optional
	PgRestoreExtraOptions []string `json:"pgRestoreExtraOptions,omitempty"`

	// Custom options to pass to the `pg_restore` command during the `pre-data`
	// section. This setting overrides the generic `pgRestoreExtraOptions` value.
	//
	// IMPORTANT: Use with caution. The operator does not validate these options,
	// and certain flags may interfere with its intended functionality or design.
	// You are responsible for ensuring that the provided options are compatible
	// with your environment and desired behavior.
	//
	// +optional
	PgRestorePredataOptions []string `json:"pgRestorePredataOptions,omitempty"`

	// Custom options to pass to the `pg_restore` command during the `data`
	// section. This setting overrides the generic `pgRestoreExtraOptions` value.
	//
	// IMPORTANT: Use with caution. The operator does not validate these options,
	// and certain flags may interfere with its intended functionality or design.
	// You are responsible for ensuring that the provided options are compatible
	// with your environment and desired behavior.
	//
	// +optional
	PgRestoreDataOptions []string `json:"pgRestoreDataOptions,omitempty"`

	// Custom options to pass to the `pg_restore` command during the `post-data`
	// section. This setting overrides the generic `pgRestoreExtraOptions` value.
	//
	// IMPORTANT: Use with caution. The operator does not validate these options,
	// and certain flags may interfere with its intended functionality or design.
	// You are responsible for ensuring that the provided options are compatible
	// with your environment and desired behavior.
	//
	// +optional
	PgRestorePostdataOptions []string `json:"pgRestorePostdataOptions,omitempty"`
}

// ImportSource describes the source for the logical snapshot
type ImportSource struct {
	// The name of the externalCluster used for import
	ExternalCluster string `json:"externalCluster"`
}

// SQLRefs holds references to ConfigMaps or Secrets
// containing SQL files. The references are processed in a specific order:
// first, all Secrets are processed, followed by all ConfigMaps.
// Within each group, the processing order follows the sequence specified
// in their respective arrays.
type SQLRefs struct {
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

	// Configuration of the storage for PostgreSQL tablespaces
	// +optional
	TablespaceStorage map[string]corev1.TypedLocalObjectReference `json:"tablespaceStorage,omitempty"`
}

// BackupSource contains the backup we need to restore from, plus some
// information that could be needed to correctly restore it.
type BackupSource struct {
	machineryapi.LocalObjectReference `json:",inline"`
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

	// The target time as a timestamp in RFC3339 format or PostgreSQL timestamp format.
	// Timestamps without an explicit timezone are interpreted as UTC.
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

// StorageConfiguration is the configuration used to create and reconcile PVCs,
// usable for WAL volumes, PGDATA volumes, or tablespaces
type StorageConfiguration struct {
	// StorageClass to use for PVCs. Applied after
	// evaluating the PVC template, if available.
	// If not specified, the generated PVCs will use the
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

// TablespaceConfiguration is the configuration of a tablespace, and includes
// the storage specification for the tablespace
type TablespaceConfiguration struct {
	// The name of the tablespace
	Name string `json:"name"`

	// The storage configuration for the tablespace
	Storage StorageConfiguration `json:"storage"`

	// Owner is the PostgreSQL user owning the tablespace
	// +optional
	Owner DatabaseRoleRef `json:"owner,omitempty"`

	// When set to true, the tablespace will be added as a `temp_tablespaces`
	// entry in PostgreSQL, and will be available to automatically house temp
	// database objects, or other temporary files. Please refer to PostgreSQL
	// documentation for more information on the `temp_tablespaces` GUC.
	// +optional
	// +kubebuilder:default:=false
	Temporary bool `json:"temporary,omitempty"`
}

// DatabaseRoleRef is a reference an a role available inside PostgreSQL
type DatabaseRoleRef struct {
	// +optional
	Name string `json:"name,omitempty"`
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
	//
	// Deprecated: This feature will be removed in an upcoming release. If
	// you need this functionality, you can create a PodMonitor manually.
	// +optional
	EnablePodMonitor bool `json:"enablePodMonitor,omitempty"`

	// Configure TLS communication for the metrics endpoint.
	// Changing tls.enabled option will force a rollout of all instances.
	// +optional
	TLSConfig *ClusterMonitoringTLSConfiguration `json:"tls,omitempty"`

	// The list of metric relabelings for the `PodMonitor`. Applied to samples before ingestion.
	//
	// Deprecated: This feature will be removed in an upcoming release. If
	// you need this functionality, you can create a PodMonitor manually.
	// +optional
	PodMonitorMetricRelabelConfigs []monitoringv1.RelabelConfig `json:"podMonitorMetricRelabelings,omitempty"`

	// The list of relabelings for the `PodMonitor`. Applied to samples before scraping.
	//
	// Deprecated: This feature will be removed in an upcoming release. If
	// you need this functionality, you can create a PodMonitor manually.
	// +optional
	PodMonitorRelabelConfigs []monitoringv1.RelabelConfig `json:"podMonitorRelabelings,omitempty"`

	// The interval during which metrics computed from queries are considered current.
	// Once it is exceeded, a new scrape will trigger a rerun
	// of the queries.
	// If not set, defaults to 30 seconds, in line with Prometheus scraping defaults.
	// Setting this to zero disables the caching mechanism and can cause heavy load on the PostgreSQL server.
	// +optional
	MetricsQueriesTTL *metav1.Duration `json:"metricsQueriesTTL,omitempty"`
}

// ClusterMonitoringTLSConfiguration is the type containing the TLS configuration
// for the cluster's monitoring
type ClusterMonitoringTLSConfiguration struct {
	// Enable TLS for the monitoring endpoint.
	// Changing this option will force a rollout of all instances.
	// +kubebuilder:default:=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
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

	// The reference to the password to be used to connect to the server.
	// If a password is provided, CloudNativePG creates a PostgreSQL
	// passfile at `/controller/external/NAME/pass` (where "NAME" is the
	// cluster's name). This passfile is automatically referenced in the
	// connection string when establishing a connection to the remote
	// PostgreSQL server from the current PostgreSQL `Cluster`. This ensures
	// secure and efficient password management for external clusters.
	// +optional
	Password *corev1.SecretKeySelector `json:"password,omitempty"`

	// The configuration for the barman-cloud tool suite
	// +optional
	BarmanObjectStore *BarmanObjectStoreConfiguration `json:"barmanObjectStore,omitempty"`

	// The configuration of the plugin that is taking care
	// of WAL archiving and backups for this external cluster
	PluginConfiguration *PluginConfiguration `json:"plugin,omitempty"`
}

// EnsureOption represents whether we should enforce the presence or absence of
// a Role in a PostgreSQL instance
type EnsureOption string

// values taken by EnsureOption
const (
	EnsurePresent EnsureOption = "present"
	EnsureAbsent  EnsureOption = "absent"
)

// ServiceSelectorType describes a valid value for generating the service selectors.
// It indicates which type of service the selector applies to, such as read-write, read, or read-only
// +kubebuilder:validation:Enum=rw;r;ro
type ServiceSelectorType string

// Constants representing the valid values for ServiceSelectorType.
const (
	// ServiceSelectorTypeRW selects the read-write service.
	ServiceSelectorTypeRW ServiceSelectorType = "rw"
	// ServiceSelectorTypeR selects the read service.
	ServiceSelectorTypeR ServiceSelectorType = "r"
	// ServiceSelectorTypeRO selects the read-only service.
	ServiceSelectorTypeRO ServiceSelectorType = "ro"
)

// ServiceUpdateStrategy describes how the changes to the managed service should be handled
// +kubebuilder:validation:Enum=patch;replace
type ServiceUpdateStrategy string

const (
	// ServiceUpdateStrategyPatch applies a patch deriving from the differences of the actual service and the expect one
	ServiceUpdateStrategyPatch = "patch"
	// ServiceUpdateStrategyReplace deletes the existing service and recreates it when a difference is detected
	ServiceUpdateStrategyReplace = "replace"
)

// ManagedServices represents the services managed by the cluster.
type ManagedServices struct {
	// DisabledDefaultServices is a list of service types that are disabled by default.
	// Valid values are "r", and "ro", representing read, and read-only services.
	// +optional
	DisabledDefaultServices []ServiceSelectorType `json:"disabledDefaultServices,omitempty"`
	// Additional is a list of additional managed services specified by the user.
	// +optional
	Additional []ManagedService `json:"additional,omitempty"`
}

// ManagedService represents a specific service managed by the cluster.
// It includes the type of service and its associated template specification.
type ManagedService struct {
	// SelectorType specifies the type of selectors that the service will have.
	// Valid values are "rw", "r", and "ro", representing read-write, read, and read-only services.
	SelectorType ServiceSelectorType `json:"selectorType"`

	// UpdateStrategy describes how the service differences should be reconciled
	// +kubebuilder:default:="patch"
	// +optional
	UpdateStrategy ServiceUpdateStrategy `json:"updateStrategy,omitempty"`

	// ServiceTemplate is the template specification for the service.
	ServiceTemplate ServiceTemplateSpec `json:"serviceTemplate"`
}

// ManagedConfiguration represents the portions of PostgreSQL that are managed
// by the instance manager
type ManagedConfiguration struct {
	// Database roles managed by the `Cluster`
	// +optional
	Roles []RoleConfiguration `json:"roles,omitempty"`
	// Services roles managed by the `Cluster`
	// +optional
	Services *ManagedServices `json:"services,omitempty"`
}

// PluginConfiguration specifies a plugin that need to be loaded for this
// cluster to be reconciled
type PluginConfiguration struct {
	// Name is the plugin name
	Name string `json:"name"`

	// Enabled is true if this plugin will be used
	// +kubebuilder:default:=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Marks the plugin as the WAL archiver. At most one plugin can be
	// designated as a WAL archiver. This cannot be enabled if the
	// `.spec.backup.barmanObjectStore` configuration is present.
	// +kubebuilder:default:=false
	// +optional
	IsWALArchiver *bool `json:"isWALArchiver,omitempty"`

	// Parameters is the configuration of the plugin
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

// PluginStatus is the status of a loaded plugin
type PluginStatus struct {
	// Name is the name of the plugin
	Name string `json:"name"`

	// Version is the version of the plugin loaded by the
	// latest reconciliation loop
	Version string `json:"version"`

	// Capabilities are the list of capabilities of the
	// plugin
	// +optional
	Capabilities []string `json:"capabilities,omitempty"`

	// OperatorCapabilities are the list of capabilities of the
	// plugin regarding the reconciler
	// +optional
	OperatorCapabilities []string `json:"operatorCapabilities,omitempty"`

	// WALCapabilities are the list of capabilities of the
	// plugin regarding the WAL management
	// +optional
	WALCapabilities []string `json:"walCapabilities,omitempty"`

	// BackupCapabilities are the list of capabilities of the
	// plugin regarding the Backup management
	// +optional
	BackupCapabilities []string `json:"backupCapabilities,omitempty"`

	// RestoreJobHookCapabilities are the list of capabilities of the
	// plugin regarding the RestoreJobHook management
	// +optional
	RestoreJobHookCapabilities []string `json:"restoreJobHookCapabilities,omitempty"`

	// Status contain the status reported by the plugin through the SetStatusInCluster interface
	// +optional
	Status string `json:"status,omitempty"`
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

// Cluster defines the API schema for a highly available PostgreSQL database cluster
// managed by CloudNativePG.
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
	// +optional
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

	// The resource versions of the external cluster secrets
	// +optional
	ExternalClusterSecretVersions map[string]string `json:"externalClusterSecretVersion,omitempty"`

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

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
