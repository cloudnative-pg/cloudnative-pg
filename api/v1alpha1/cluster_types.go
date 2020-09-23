/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/versions"
)

const (
	// SuperUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the PostgreSQL superuser secret
	SuperUserSecretSuffix = "-superuser"

	// ApplicationUserSecretSuffix is the suffix appended to the cluster name to
	// get the name of the application user secret
	ApplicationUserSecretSuffix = "-app"

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
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Description of this PostgreSQL cluster
	Description string `json:"description,omitempty"`

	// Name of the container image
	// +kubebuilder:validation:MinLength=0
	ImageName string `json:"imageName,omitempty"`

	// Number of instances required in the cluster
	// +kubebuilder:validation:Minimum=1
	Instances int32 `json:"instances"`

	// Configuration of the PostgreSQL server
	// +optional
	PostgresConfiguration PostgresConfiguration `json:"postgresql,omitempty"`

	// Configuration from the application point of view
	ApplicationConfiguration ApplicationConfiguration `json:"applicationConfiguration"`

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

	// Resources requirements of every generated Pod
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

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// Total number of instances in the cluster
	Instances int32 `json:"instances,omitempty"`

	// Total number of ready instances in the cluster
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// ID of the latest generated node (used to avoid node name clashing)
	LatestGeneratedNode int32 `json:"latestGeneratedNode,omitempty"`

	// Current primary instance
	CurrentPrimary string `json:"currentPrimary,omitempty"`

	// Target primary instance, this is different from the previous one
	// during a switchover or a failover
	TargetPrimary string `json:"targetPrimary,omitempty"`

	// List of all the PVCs created by this bdrGroup and still available
	// which are not attached to a Pod
	DanglingPVC []string `json:"danglingPVC,omitempty"`
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

// ApplicationConfiguration is the configuration required by the application
type ApplicationConfiguration struct {
	// Name of the database used by the application
	// +kubebuilder:validation:MinLength=1
	Database string `json:"database"`

	// Name of the owner of the database in the instance to be used
	// by applications.
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`
}

// StorageConfiguration is the configuration of the storage of the PostgreSQL instances
type StorageConfiguration struct {
	// StorageClass to use for database data (PGDATA). Applied after
	// evaluating the PVC template, if available.
	// If not specified, generated PVCs will be satisfied by the
	// default storage class
	// +optional
	StorageClass *string `json:"storageClass"`

	// Size of the storage. Required if not already specified in the PVC template.
	Size resource.Quantity `json:"size"`

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

// BackupConfiguration contains the backup configuration when the backup
// is available
type BackupConfiguration struct {
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

	// The configuration for the backup of the WAL stream
	Wal *WalBackupConfiguration `json:"wal,omitempty"`

	// The configuration to be used to backup the data files
	Data *DataBackupConfiguration `json:"data,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Whenever to compress files or not
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that)
	Encryption EncryptionType `json:"encryption,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// Whenever to compress files or not
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that)
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Whenever to force the initial checkpoint to be done as quickly
	// as possible
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`

	// The number of jobs to be used to upload the backup, defaults
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

// Cluster is the Schema for the postgresqls API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
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
	return fmt.Sprintf("%v%v", cluster.Name, SuperUserSecretSuffix)
}

// GetApplicationSecretName get the name of the secret of the application
func (cluster *Cluster) GetApplicationSecretName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
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

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
