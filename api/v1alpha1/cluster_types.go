/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

const (
	// ReplicationSecretSuffix is the suffix appended to the cluster name to
	// get the name of the generated replication secret for PostgreSQL
	ReplicationSecretSuffix = "-replication" // #nosec

	// SuperUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the PostgreSQL superuser secret
	SuperUserSecretSuffix = "-superuser"

	// ApplicationUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the application user secret
	ApplicationUserSecretSuffix = "-app"

	// CaSecretSuffix is the suffix appended to the secret containing
	// the generated CA for the cluster
	CaSecretSuffix = "-ca"

	// ServerSecretSuffix is the suffix appended to the secret containing
	// the generated server secret for PostgreSQL
	ServerSecretSuffix = "-server"

	// ServiceAnySuffix is the suffix appended to the cluster name to get the
	// service name for every node (including non-ready ones)
	ServiceAnySuffix = "-any"

	// ServiceReadSuffix is the suffix appended to the cluster name to get the
	// service name for every ready node that you can use to read data
	ServiceReadSuffix = "-r"

	// ServiceReadWriteSuffix is the suffix appended to the cluster name to get
	// the se service name for every node that you can use to read and write
	// data
	ServiceReadWriteSuffix = "-rw"

	// StreamingReplicationUser is the name of the user we'll use for
	// streaming replication purposes
	StreamingReplicationUser = "streaming_replica"

	// defaultPostgresUID is the default UID which is used by PostgreSQL
	defaultPostgresUID = 26

	// defaultPostgresGID is the default GID which is used by PostgreSQL
	defaultPostgresGID = 26
)

// MetaSubset contains a subset of the standard object's metadata.
// TODO: Dissmiss this type if we can in the future
type MetaSubset struct {
	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// PodTemplateSpec describes the data a pod should have when created from a template.
// TODO: Dissmiss this type if we can in the future
type PodTemplateSpec struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	Meta MetaSubset `json:"metadata,omitempty"`

	// Specification of the desired behavior of the pod.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec corev1.PodSpec `json:"spec,omitempty"`
}

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Description of this PostgreSQL cluster
	Description string `json:"description,omitempty"`

	// Name of the container image
	// +kubebuilder:validation:MinLength=0
	ImageName string `json:"imageName,omitempty"`

	// The UID of the `postgres` user inside the image, defaults to `26`
	PostgresUID int64 `json:"postgresUID,omitempty"`

	// The GID of the `postgres` user inside the image, defaults to `26`
	PostgresGID int64 `json:"postgresGID,omitempty"`

	// Number of instances required in the cluster
	// +kubebuilder:validation:Minimum=1
	Instances int32 `json:"instances"`

	// Minimum number of instances required in synchronous replication with the
	// primary. Undefined or 0 allow writes to complete when no standby is
	// available.
	MinSyncReplicas int32 `json:"minSyncReplicas,omitempty"`

	// The target value for the synchronous replication quorum, that can be
	// decreased if the number of ready standbys is lower than this.
	// Undefined or 0 disable synchronous replication.
	MaxSyncReplicas int32 `json:"maxSyncReplicas,omitempty"`

	// Configuration of the PostgreSQL server
	// +optional
	PostgresConfiguration PostgresConfiguration `json:"postgresql,omitempty"`

	// Instructions to bootstrap this cluster
	// +optional
	Bootstrap *BootstrapConfiguration `json:"bootstrap,omitempty"`

	// The secret containing the superuser password. If not defined a new
	// secret will be created with a randomly generated password
	// +optional
	SuperuserSecret *corev1.LocalObjectReference `json:"superuserSecret,omitempty"`

	// The list of pull secrets to be used to pull the images
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Configuration of the storage of the instances
	// +optional
	StorageConfiguration StorageConfiguration `json:"storage,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance to
	// successfully start up (default 30)
	MaxStartDelay int32 `json:"startDelay,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance node to
	// gracefully shutdown (default 30)
	MaxStopDelay int32 `json:"stopDelay,omitempty"`

	// Affinity/Anti-affinity rules for Pods
	// +optional
	Affinity AffinityConfiguration `json:"affinity,omitempty"`

	// This contains a Pod template which is applied when creating
	// the actual instances
	// +optional
	PodTemplate *PodTemplateSpec `json:"podTemplate,omitempty"`

	// Resources requirements of every generated Pod. Please refer to
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// for more information.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Strategy to follow to upgrade the primary server during a rolling
	// update procedure, after all replicas have been successfully updated:
	// it can be automated (`unsupervised` - default) or manual (`supervised`)
	PrimaryUpdateStrategy PrimaryUpdateStrategy `json:"primaryUpdateStrategy,omitempty"`

	// The configuration to be used for backups
	Backup *BackupConfiguration `json:"backup,omitempty"`

	// Define a maintenance window for the Kubernetes nodes
	NodeMaintenanceWindow *NodeMaintenanceWindow `json:"nodeMaintenanceWindow,omitempty"`
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

	// PhaseUpgradeFailed used for failures in upgrade
	PhaseUpgradeFailed = "Cluster upgrade failed"

	// PhaseWaitingForUser set the status to wait for an action from the user
	PhaseWaitingForUser = "Waiting for user action"

	// PhaseHealthy for a cluster doing nothing
	PhaseHealthy = "Cluster in healthy state"
)

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// Total number of instances in the cluster
	Instances int32 `json:"instances,omitempty"`

	// Total number of ready instances in the cluster
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// Instances status
	InstancesStatus map[utils.PodStatus][]string `json:"instancesStatus,omitempty"`

	// ID of the latest generated node (used to avoid node name clashing)
	LatestGeneratedNode int32 `json:"latestGeneratedNode,omitempty"`

	// Current primary instance
	CurrentPrimary string `json:"currentPrimary,omitempty"`

	// Target primary instance, this is different from the previous one
	// during a switchover or a failover
	TargetPrimary string `json:"targetPrimary,omitempty"`

	// How many PVCs have been created by this cluster
	PVCCount int32 `json:"pvcCount,omitempty"`

	// How many Jobs have been created by this cluster
	JobCount int32 `json:"jobCount,omitempty"`

	// List of all the PVCs created by this cluster and still available
	// which are not attached to a Pod
	DanglingPVC []string `json:"danglingPVC,omitempty"`

	// Current write pod
	WriteService string `json:"writeService,omitempty"`

	// Current list of read pods
	ReadService string `json:"readService,omitempty"`

	// Current phase of the cluster
	Phase string `json:"phase,omitempty"`

	// Reason for the current phase
	PhaseReason string `json:"phaseReason,omitempty"`
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
// This option is only useful when using local storage, as the Pods
// can't be freely moved between nodes in that configuration.
type NodeMaintenanceWindow struct {
	// Is there a node maintenance activity in progress?
	InProgress bool `json:"inProgress"`

	// Reuse the existing PVC (wait for the node to come
	// up again) or not (recreate it elsewhere)
	// +optional
	ReusePVC *bool `json:"reusePVC"`
}

// PrimaryUpdateStrategy contains the strategy to follow when upgrading
// the primary server of the cluster as part of rolling updates
type PrimaryUpdateStrategy string

const (
	// PrimaryUpdateStrategySupervised means that the operator need to wait for the
	// user to manually issue a switchover request before updating the primary
	// server (`supervised`)
	PrimaryUpdateStrategySupervised = "supervised"

	// PrimaryUpdateStrategyUnsupervised means that the operator will switchover
	// to another updated replica and then automatically update the primary server
	// (`unsupervised`, default)
	PrimaryUpdateStrategyUnsupervised = "unsupervised"
)

// PostgresConfiguration defines the PostgreSQL configuration
type PostgresConfiguration struct {
	// PostgreSQL configuration options (postgresql.conf)
	Parameters map[string]string `json:"parameters,omitempty"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	// +optional
	PgHBA []string `json:"pg_hba,omitempty"`
}

// BootstrapConfiguration contains information about how to create the PostgreSQL
// cluster. Only a single bootstrap method can be defined among the supported
// ones. `initdb` will be used as the bootstrap method if left
// unspecified. Refer to the Bootstrap page of the documentation for more
// information.
type BootstrapConfiguration struct {
	// Bootstrap the cluster via initdb
	InitDB *BootstrapInitDB `json:"initdb,omitempty"`

	// Bootstrap the cluster from a backup
	Recovery *BootstrapRecovery `json:"recovery,omitempty"`
}

// BootstrapInitDB is the configuration of the bootstrap process when
// initdb is used
// Refer to the Bootstrap page of the documentation for more information.
type BootstrapInitDB struct {
	// Name of the database used by the application. Default: `app`.
	// +optional
	Database string `json:"database"`

	// Name of the owner of the database in the instance to be used
	// by applications. Defaults to the value of the `database` key.
	// +optional
	Owner string `json:"owner"`

	// Name of the secret containing the initial credentials for the
	// owner of the user database. If empty a new secret will be
	// created from scratch
	// +optional
	Secret *corev1.LocalObjectReference `json:"secret,omitempty"`

	// The list of options that must be passed to initdb
	// when creating the cluster
	Options []string `json:"options,omitempty"`
}

// BootstrapRecovery contains the configuration required to restore
// the backup with the specified name and, after having changed the password
// with the one chosen for the superuser, will use it to bootstrap a full
// cluster cloning all the instances from the restored primary.
// Refer to the Bootstrap page of the documentation for more information.
type BootstrapRecovery struct {
	// The backup we need to restore
	Backup corev1.LocalObjectReference `json:"backup"`

	// By default the recovery will end as soon as a consistent state is
	// reached: in this case that means at the end of a backup.
	// This option allows to fine tune the recovery process
	// +optional
	RecoveryTarget *RecoveryTarget `json:"recoveryTarget,omitempty"`
}

// RecoveryTarget allows to configure the moment where the recovery process
// will stop. All the target options except TargetTLI are mutually exclusive.
type RecoveryTarget struct {
	// The target timeline ("latest", "current" or a positive integer)
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

	// The target time, in any unambiguous representation
	// allowed by PostgreSQL
	TargetTime string `json:"targetTime,omitempty"`

	// End recovery as soon as a consistent state is reached
	TargetImmediate *bool `json:"targetImmediate,omitempty"`

	// Set the target to be exclusive (defaults to true)
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
	Size string `json:"size"`

	// Resize existent PVCs, defaults to true
	// +optional
	ResizeInUseVolumes *bool `json:"resizeInUseVolumes,omitempty"`

	// Template to be used to generate the Persistent Volume Claim
	// +optional
	PersistentVolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"pvcTemplate,omitempty"`
}

// AffinityConfiguration contains the info we need to create the
// affinity rules for Pods
type AffinityConfiguration struct {
	// Should we enable anti affinity or not?
	EnablePodAntiAffinity bool `json:"enablePodAntiAffinity"`

	// TopologyKey to use for anti-affinity configuration. See k8s documentation
	// for more info on that
	// +optional
	TopologyKey string `json:"topologyKey"`
}

// RollingUpdateStatus contains the information about an instance which is
// being updated
type RollingUpdateStatus struct {
	// The image which we put into the Pod
	ImageName string `json:"imageName"`

	// When the update has been started
	StartedAt metav1.Time `json:"startedAt,omitempty"`
}

// CompressionType encapsulates the available types of compression
type CompressionType string

const (
	// CompressionTypeNone means no compression is performed
	CompressionTypeNone = ""

	// CompressionTypeGzip means gzip compression is performed
	CompressionTypeGzip = "gzip"

	// CompressionTypeBzip2 means bzip2 compression is performed
	CompressionTypeBzip2 = "bzip2"
)

// EncryptionType encapsulated the available types of encryption
type EncryptionType string

const (
	// EncryptionTypeNone means just use the bucket configuration
	EncryptionTypeNone = ""

	// EncryptionTypeAES256 means to use AES256 encryption
	EncryptionTypeAES256 = "AES256"

	// EncryptionTypeNoneAWSKMS means to use aws:kms encryption
	EncryptionTypeNoneAWSKMS = "aws:kms"
)

// BarmanObjectStoreConfiguration contains the backup configuration
// using Barman against an S3-compatible object storage
type BarmanObjectStoreConfiguration struct {
	// The credentials to use to upload data to S3
	S3Credentials S3Credentials `json:"s3Credentials"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	EndpointURL string `json:"endpointURL,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data
	//+kubebuilder:validation:MinLength=1
	DestinationPath string `json:"destinationPath"`

	// The server name on S3, the cluster name is used if this
	// parameter is omitted
	ServerName string `json:"serverName,omitempty"`

	// The configuration for the backup of the WAL stream.
	// When not defined, WAL files will be stored uncompressed and may be
	// unencrypted in the object store, according to the bucket default policy.
	Wal *WalBackupConfiguration `json:"wal,omitempty"`

	// The configuration to be used to backup the data files
	// When not defined, base backups files will be stored uncompressed and may
	// be unencrypted in the object store, according to the bucket default
	// policy.
	Data *DataBackupConfiguration `json:"data,omitempty"`
}

// BackupConfiguration defines how the backup of the cluster are taken.
// Currently the only supported backup method is barmanObjectStore.
// For details and examples refer to the Backup and Recovery section of the
// documentation
type BackupConfiguration struct {
	// The configuration for the barman-cloud tool suite
	BarmanObjectStore *BarmanObjectStoreConfiguration `json:"barmanObjectStore,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Compress a WAL file before sending it to the object store. Available
	// options are empty string (no compression, default), `gzip` or `bzip2`.
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	Encryption EncryptionType `json:"encryption,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// Compress a backup file (a tar file per tablespace) while streaming it
	// to the object store. Available options are empty string (no
	// compression, default), `gzip` or `bzip2`.
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`

	// The number of parallel jobs to be used to upload the backup, defaults
	// to 2
	Jobs *int32 `json:"jobs,omitempty"`
}

// S3Credentials is the type for the credentials to be used to upload
// files to S3
type S3Credentials struct {
	// The reference to the access key id
	AccessKeyIDReference corev1.SecretKeySelector `json:"accessKeyId"`

	// The reference to the secret access key
	SecretAccessKeyReference corev1.SecretKeySelector `json:"secretAccessKey"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.instances,statuspath=.status.instances
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Instances",type="integer",JSONPath=".status.instances",description="Number of instances"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyInstances",description="Number of ready instances"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="Cluster current status"
// +kubebuilder:printcolumn:name="Primary",type="string",JSONPath=".status.currentPrimary",description="Primary pod"

// Cluster is the Schema for the postgresql API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of the cluster.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec ClusterSpec `json:"spec,omitempty"`
	// Most recently observed status of the cluster. This data may not be up
	// to date. Populated by the system. Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of clusters
	Items []Cluster `json:"items"`
}

// GetImageName get the name of the image that should be used
// to create the pods
func (cluster *Cluster) GetImageName() string {
	if len(cluster.Spec.ImageName) > 0 {
		return cluster.Spec.ImageName
	}

	return versions.GetDefaultImageName()
}

// GetSuperuserSecretName get the secret name of the PostgreSQL superuser
func (cluster *Cluster) GetSuperuserSecretName() string {
	if cluster.Spec.SuperuserSecret != nil &&
		cluster.Spec.SuperuserSecret.Name != "" {
		return cluster.Spec.SuperuserSecret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, SuperUserSecretSuffix)
}

// GetReplicationSecretName get the name of the secret for the replication user
func (cluster *Cluster) GetReplicationSecretName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ReplicationSecretSuffix)
}

// GetApplicationSecretName get the name of the secret of the application
func (cluster *Cluster) GetApplicationSecretName() string {
	if cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.InitDB != nil &&
		cluster.Spec.Bootstrap.InitDB.Secret != nil &&
		cluster.Spec.Bootstrap.InitDB.Secret.Name != "" {
		return cluster.Spec.Bootstrap.InitDB.Secret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
}

// GetCASecretName get the name of the secret containing the CA
// of the cluster
func (cluster *Cluster) GetCASecretName() string {
	return fmt.Sprintf("%v%v", cluster.Name, CaSecretSuffix)
}

// GetServerSecretName get the name of the secret containing the
// certificate that is used for the PostgreSQL servers
func (cluster *Cluster) GetServerSecretName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServerSecretSuffix)
}

// GetServiceAnyName return the name of the service that is used as DNS
// domain for all the nodes, even if they are not ready
func (cluster *Cluster) GetServiceAnyName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceAnySuffix)
}

// GetServiceReadName return the name of the service that is used for
// read-only transactions
func (cluster *Cluster) GetServiceReadName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadSuffix)
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
	return 30
}

// GetMaxStopDelay get the amount of time PostgreSQL has to stop with -m smart
func (cluster *Cluster) GetMaxStopDelay() int32 {
	if cluster.Spec.MaxStopDelay > 0 {
		return cluster.Spec.MaxStopDelay
	}
	return 30
}

// GetPrimaryUpdateStrategy get the cluster primary update strategy,
// defaulting to switchover
func (cluster *Cluster) GetPrimaryUpdateStrategy() PrimaryUpdateStrategy {
	strategy := cluster.Spec.PrimaryUpdateStrategy
	if strategy == "" {
		return PrimaryUpdateStrategyUnsupervised
	}

	return strategy
}

// IsNodeMaintenanceWindowInProgress check if the upgrade mode is active or not
func (cluster *Cluster) IsNodeMaintenanceWindowInProgress() bool {
	return cluster.Spec.NodeMaintenanceWindow != nil && cluster.Spec.NodeMaintenanceWindow.InProgress
}

// IsNodeMaintenanceWindowReusePVC check if we are in a recovery window and
// we should reuse PVCs
func (cluster *Cluster) IsNodeMaintenanceWindowReusePVC() bool {
	if !cluster.IsNodeMaintenanceWindowInProgress() {
		return false
	}

	reusePVC := true
	if cluster.Spec.NodeMaintenanceWindow.ReusePVC != nil {
		reusePVC = *cluster.Spec.NodeMaintenanceWindow.ReusePVC
	}
	return reusePVC
}

// IsNodeMaintenanceWindowNotReusePVC check if we are in a recovery window and
// should avoid reusing PVCs
func (cluster *Cluster) IsNodeMaintenanceWindowNotReusePVC() bool {
	if !cluster.IsNodeMaintenanceWindowInProgress() {
		return false
	}

	reusePVC := true
	if cluster.Spec.NodeMaintenanceWindow.ReusePVC != nil {
		reusePVC = *cluster.Spec.NodeMaintenanceWindow.ReusePVC
	}
	return !reusePVC
}

// ShouldResizeInUseVolumes is true when we should resize PVC we already
// created
func (cluster *Cluster) ShouldResizeInUseVolumes() bool {
	if cluster.Spec.StorageConfiguration.ResizeInUseVolumes == nil {
		return true
	}

	return *cluster.Spec.StorageConfiguration.ResizeInUseVolumes
}

// ShouldCreateApplicationDatabase returns true if for this cluster,
// during the bootstrap phase, we need to create an application database
func (cluster Cluster) ShouldCreateApplicationDatabase() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	initDBParameters := cluster.Spec.Bootstrap.InitDB
	return initDBParameters.Owner != "" && initDBParameters.Database != ""
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

// BuildPostgresOptions create the list of options that
// should be added to the PostgreSQL configuration to
// recover given a certain target
func (target RecoveryTarget) BuildPostgresOptions() string {
	result := ""
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
			target.TargetName)
	}
	if target.TargetTime != "" {
		result += fmt.Sprintf(
			"recovery_target_time = '%v'\n",
			target.TargetTime)
	}
	if target.TargetImmediate != nil && *target.TargetImmediate {
		result += "recovery_target = immediate\n"
	}
	switch {
	case target.Exclusive == nil:
		result += "recovery_target_inclusive = true\n"
	case *target.Exclusive:
		result += "recovery_target_inclusive = true\n"
	default:
		result += "recovery_target_inclusive = false\n"
	}

	return result
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
