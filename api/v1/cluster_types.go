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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Description of this PostgreSQL cluster
	Description string `json:"description,omitempty"`

	// Metadata that will be inherited by all objects related to the Cluster
	InheritedMetadata *EmbeddedObjectMetadata `json:"inheritedMetadata,omitempty"`

	// Name of the container image, supporting both tags (`<image>:<tag>`)
	// and digests for deterministic and repeatable deployments
	// (`<image>:<tag>@sha256:<digestValue>`)
	ImageName string `json:"imageName,omitempty"`

	// Image pull policy.
	// One of `Always`, `Never` or `IfNotPresent`.
	// If not defined, it defaults to `IfNotPresent`.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/containers/images#updating-images
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// The UID of the `postgres` user inside the image, defaults to `26`
	// +kubebuilder:default:=26
	PostgresUID int64 `json:"postgresUID,omitempty"`

	// The GID of the `postgres` user inside the image, defaults to `26`
	// +kubebuilder:default:=26
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
	MinSyncReplicas int `json:"minSyncReplicas,omitempty"`

	// The target value for the synchronous replication quorum, that can be
	// decreased if the number of ready standbys is lower than this.
	// Undefined or 0 disable synchronous replication.
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	MaxSyncReplicas int `json:"maxSyncReplicas,omitempty"`

	// Configuration of the PostgreSQL server
	// +optional
	PostgresConfiguration PostgresConfiguration `json:"postgresql,omitempty"`

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
	// user by setting it to `NULL`. Enabled by default.
	// +kubebuilder:default:=true
	EnableSuperuserAccess *bool `json:"enableSuperuserAccess,omitempty"`

	// The configuration for the CA and related certificates
	// +optional
	Certificates *CertificatesConfiguration `json:"certificates,omitempty"`

	// The list of pull secrets to be used to pull the images
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Configuration of the storage of the instances
	// +optional
	StorageConfiguration StorageConfiguration `json:"storage,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance to
	// successfully start up (default 30)
	// +kubebuilder:default:=30
	MaxStartDelay int32 `json:"startDelay,omitempty"`

	// The time in seconds that is allowed for a PostgreSQL instance to
	// gracefully shutdown (default 30)
	// +kubebuilder:default:=30
	MaxStopDelay int32 `json:"stopDelay,omitempty"`

	// The time in seconds that is allowed for a primary PostgreSQL instance
	// to gracefully shutdown during a switchover.
	// Default value is 40000000, greater than one year in seconds,
	// big enough to simulate an infinite delay
	// +kubebuilder:default:=40000000
	MaxSwitchoverDelay int32 `json:"switchoverDelay,omitempty"`

	// Affinity/Anti-affinity rules for Pods
	// +optional
	Affinity AffinityConfiguration `json:"affinity,omitempty"`

	// Resources requirements of every generated Pod. Please refer to
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// for more information.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Strategy to follow to upgrade the primary server during a rolling
	// update procedure, after all replicas have been successfully updated:
	// it can be automated (`unsupervised` - default) or manual (`supervised`)
	// +kubebuilder:default:=unsupervised
	// +kubebuilder:validation:Enum:=unsupervised;supervised
	PrimaryUpdateStrategy PrimaryUpdateStrategy `json:"primaryUpdateStrategy,omitempty"`

	// Method to follow to upgrade the primary server during a rolling
	// update procedure, after all replicas have been successfully updated:
	// it can be with a switchover (`switchover` - default) or in-place (`restart`)
	// +kubebuilder:default:=switchover
	// +kubebuilder:validation:Enum:=switchover;restart
	PrimaryUpdateMethod PrimaryUpdateMethod `json:"primaryUpdateMethod,omitempty"`

	// The configuration to be used for backups
	Backup *BackupConfiguration `json:"backup,omitempty"`

	// Define a maintenance window for the Kubernetes nodes
	NodeMaintenanceWindow *NodeMaintenanceWindow `json:"nodeMaintenanceWindow,omitempty"`

	// The configuration of the monitoring infrastructure of this cluster
	Monitoring *MonitoringConfiguration `json:"monitoring,omitempty"`

	// The list of external clusters which are used in the configuration
	ExternalClusters []ExternalCluster `json:"externalClusters,omitempty"`

	// The instances' log level, one of the following values: error, warning, info (default), debug, trace
	// +kubebuilder:default:=info
	// +kubebuilder:validation:Enum:=error;warning;info;debug;trace
	LogLevel string `json:"logLevel,omitempty"`
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

	// PhaseOnlineUpgrading for when the instance manager is being upgraded in place
	PhaseOnlineUpgrading = "Online upgrade in progress"

	// PhaseApplyingConfiguration is set by the instance manager when a configuration
	// change is being detected
	PhaseApplyingConfiguration = "Applying configuration"
)

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// Total number of instances in the cluster
	Instances int `json:"instances,omitempty"`

	// Total number of ready instances in the cluster
	ReadyInstances int `json:"readyInstances,omitempty"`

	// Instances status
	InstancesStatus map[utils.PodStatus][]string `json:"instancesStatus,omitempty"`

	// ID of the latest generated node (used to avoid node name clashing)
	LatestGeneratedNode int `json:"latestGeneratedNode,omitempty"`

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

	// List of all the PVCs that have ResizingPVC condition.
	ResizingPVC []string `json:"resizingPVC,omitempty"`

	// List of all the PVCs that are being initialized by this cluster
	InitializingPVC []string `json:"initializingPVC,omitempty"`

	// List of all the PVCs not dangling nor initializing
	HealthyPVC []string `json:"healthyPVC,omitempty"`

	// Current write pod
	WriteService string `json:"writeService,omitempty"`

	// Current list of read pods
	ReadService string `json:"readService,omitempty"`

	// Current phase of the cluster
	Phase string `json:"phase,omitempty"`

	// Reason for the current phase
	PhaseReason string `json:"phaseReason,omitempty"`

	// The list of resource versions of the secrets
	// managed by the operator. Every change here is done in the
	// interest of the instance manager, which will refresh the
	// secret data
	SecretsResourceVersion SecretsResourceVersion `json:"secretsResourceVersion,omitempty"`

	// The list of resource versions of the configmaps,
	// managed by the operator. Every change here is done in the
	// interest of the instance manager, which will refresh the
	// configmap data
	ConfigMapResourceVersion ConfigMapResourceVersion `json:"configMapResourceVersion,omitempty"`

	// The configuration for the CA and related certificates, initialized with defaults.
	Certificates CertificatesStatus `json:"certificates,omitempty"`

	// The first recoverability point, stored as a date in RFC3339 format
	FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint,omitempty"`

	// The commit hash number of which this operator running
	CommitHash string `json:"cloudNativePGCommitHash,omitempty"`

	// The timestamp when the last actual promotion to primary has occurred
	CurrentPrimaryTimestamp string `json:"currentPrimaryTimestamp,omitempty"`

	// The timestamp when the last request for a new primary has occurred
	TargetPrimaryTimestamp string `json:"targetPrimaryTimestamp,omitempty"`

	// The integration needed by poolers referencing the cluster
	PoolerIntegrations *PoolerIntegrations `json:"poolerIntegrations,omitempty"`

	// The hash of the binary of the operator
	OperatorHash string `json:"cloudNativePGOperatorHash,omitempty"`

	// OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster
	OnlineUpdateEnabled bool `json:"onlineUpdateEnabled,omitempty"`

	// AzurePVCUpdateEnabled shows if the PVC online upgrade is enabled for this cluster
	AzurePVCUpdateEnabled bool `json:"azurePVCUpdateEnabled,omitempty"`

	// Conditions for cluster object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// These are valid conditions of a Cluster, some of the conditions could be owned by
// Instance Manager and some of them could be owned by reconciler.
const (
	// ConditionContinuousArchiving this condition archiving :owned by InstanceManager.
	ConditionContinuousArchiving ClusterConditionType = "ContinuousArchiving"
	// ConditionBackup this condition looking backup status :owned by InstanceManager.
	ConditionBackup ClusterConditionType = "LastBackupSucceeded"
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

// ClusterConditionType is of string type
type ClusterConditionType string

// EmbeddedObjectMetadata contains metadata to be inherited by all resources related to a Cluster
type EmbeddedObjectMetadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PoolerIntegrations encapsulates the needed integration for the poolers referencing the cluster
type PoolerIntegrations struct {
	PgBouncerIntegration PgBouncerIntegrationStatus `json:"pgBouncerIntegration,omitempty"`
}

// PgBouncerIntegrationStatus encapsulates the needed integration for the pgbouncer poolers referencing the cluster
type PgBouncerIntegrationStatus struct {
	Secrets []string `json:"secrets,omitempty"`
}

// ReplicaClusterConfiguration encapsulates the configuration of a replica
// cluster
type ReplicaClusterConfiguration struct {
	// If replica mode is enabled, this cluster will be a replica of an
	// existing cluster. Replica cluster can be created from a recovery
	// object store or via streaming through pg_basebackup.
	// Refer to the Replication page of the documentation for more information.
	// +optional
	Enabled bool `json:"enabled"`

	// The name of the external cluster which is the replication origin
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source"`
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
	// Is there a node maintenance activity in progress?
	// +kubebuilder:default:=false
	InProgress bool `json:"inProgress"`

	// Reuse the existing PVC (wait for the node to come
	// up again) or not (recreate it elsewhere - when `instances` >1)
	// +optional
	// +kubebuilder:default:=true
	ReusePVC *bool `json:"reusePVC"`
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
	DefaultMaxSwitchoverDelay = 40000000
)

// PostgresConfiguration defines the PostgreSQL configuration
type PostgresConfiguration struct {
	// PostgreSQL configuration options (postgresql.conf)
	Parameters map[string]string `json:"parameters,omitempty"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	// +optional
	PgHBA []string `json:"pg_hba,omitempty"`

	// Specifies the maximum number of seconds to wait when promoting an instance to primary.
	// Default value is 40000000, greater than one year in seconds,
	// big enough to simulate an infinite timeout
	// +optional
	PgCtlTimeoutForPromotion int32 `json:"promotionTimeout,omitempty"`

	// Lists of shared preload libraries to add to the default ones
	// +optional
	AdditionalLibraries []string `json:"shared_preload_libraries,omitempty"`

	// Options to specify LDAP configuration
	// +optional
	LDAP *LDAPConfig `json:"ldap,omitempty"`
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

	// Bootstrap the cluster taking a physical backup of another compatible
	// PostgreSQL instance
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
	Server string `json:"server,omitempty"`
	// LDAP server port
	Port int `json:"port,omitempty"`

	// LDAP schema to be used, possible options are `ldap` and `ldaps`
	// +kubebuilder:validation:Enum=ldap;ldaps
	Scheme LDAPScheme `json:"scheme,omitempty"`

	// Set to 1 to enable LDAP over TLS
	TLS bool `json:"tls,omitempty"`

	// Bind as authentication configuration
	BindAsAuth *LDAPBindAsAuth `json:"bindAsAuth,omitempty"`

	// Bind+Search authentication configuration
	BindSearchAuth *LDAPBindSearchAuth `json:"bindSearchAuth,omitempty"`
}

// LDAPBindAsAuth provides the required fields to use the
// bind authentication for LDAP
type LDAPBindAsAuth struct {
	// Prefix for the bind authentication option
	Prefix string `json:"prefix,omitempty"`
	// Suffix for the bind authentication option
	Suffix string `json:"suffix,omitempty"`
}

// LDAPBindSearchAuth provides the required fields to use
// the bind+search LDAP authentication process
type LDAPBindSearchAuth struct {
	// Root DN to begin the user search
	BaseDN string `json:"baseDN,omitempty"`
	// DN of the user to bind to the directory
	BindDN string `json:"bindDN,omitempty"`
	// Secret with the password for the user to bind to the directory
	BindPassword *corev1.SecretKeySelector `json:"bindPassword,omitempty"`

	// Attribute to match against the username
	SearchAttribute string `json:"searchAttribute,omitempty"`
	// Search filter to use when doing the search+bind authentication
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
	ServerCASecret string `json:"serverCASecret,omitempty"`

	// The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as
	// `ssl_cert_file` and `ssl_key_file` so that clients can connect to postgres securely.
	// If not defined, ServerCASecret must provide also `ca.key` and a new secret will be
	// created using the provided CA.
	ServerTLSSecret string `json:"serverTLSSecret,omitempty"`

	// The secret of type kubernetes.io/tls containing the client certificate to authenticate as
	// the `streaming_replica` user.
	// If not defined, ClientCASecret must provide also `ca.key`, and a new secret will be
	// created using the provided CA.
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
	ClientCASecret string `json:"clientCASecret,omitempty"`

	// The list of the server alternative DNS names to be added to the generated server TLS certificates, when required.
	ServerAltDNSNames []string `json:"serverAltDNSNames,omitempty"`
}

// CertificatesStatus contains configuration certificates and related expiration dates.
type CertificatesStatus struct {
	// Needed configurations to handle server certificates, initialized with default values, if needed.
	CertificatesConfiguration `json:",inline"`

	// Expiration dates for all certificates.
	Expirations map[string]string `json:"expirations,omitempty"`
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
	Secret *LocalObjectReference `json:"secret,omitempty"`

	// The list of options that must be passed to initdb when creating the cluster.
	// Deprecated: This could lead to inconsistent configurations,
	// please use the explicit provided parameters instead.
	// If defined, explicit values will be ignored.
	Options []string `json:"options,omitempty"`

	// Whether the `-k` option should be passed to initdb,
	// enabling checksums on data pages (default: `false`)
	DataChecksums *bool `json:"dataChecksums,omitempty"`

	// The value to be passed as option `--encoding` for initdb (default:`UTF8`)
	Encoding string `json:"encoding,omitempty"`

	// The value to be passed as option `--lc-collate` for initdb (default:`C`)
	LocaleCollate string `json:"localeCollate,omitempty"`

	// The value to be passed as option `--lc-ctype` for initdb (default:`C`)
	LocaleCType string `json:"localeCType,omitempty"`

	// The value in megabytes (1 to 1024) to be passed to the `--wal-segsize`
	// option for initdb (default: empty, resulting in PostgreSQL default: 16MB)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1024
	WalSegmentSize int `json:"walSegmentSize,omitempty"`

	// List of SQL queries to be executed as a superuser immediately
	// after the cluster has been created - to be used with extreme care
	// (by default empty)
	PostInitSQL []string `json:"postInitSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the application
	// database right after is created - to be used with extreme care
	// (by default empty)
	PostInitApplicationSQL []string `json:"postInitApplicationSQL,omitempty"`

	// List of SQL queries to be executed as a superuser in the `template1`
	// after the cluster has been created - to be used with extreme care
	// (by default empty)
	PostInitTemplateSQL []string `json:"postInitTemplateSQL,omitempty"`
}

// BootstrapRecovery contains the configuration required to restore
// the backup with the specified name and, after having changed the password
// with the one chosen for the superuser, will use it to bootstrap a full
// cluster cloning all the instances from the restored primary.
// Refer to the Bootstrap page of the documentation for more information.
type BootstrapRecovery struct {
	// The backup we need to restore
	Backup *BackupSource `json:"backup,omitempty"`

	// The external cluster whose backup we will restore. This is also
	// used as the name of the folder under which the backup is stored,
	// so it must be set to the name of the source cluster
	Source string `json:"source,omitempty"`

	// By default, the recovery process applies all the available
	// WAL files in the archive (full recovery). However, you can also
	// end the recovery as soon as a consistent state is reached or
	// recover to a point-in-time (PITR) by specifying a `RecoveryTarget` object,
	// as expected by PostgreSQL (i.e., timestamp, transaction Id, LSN, ...).
	// More info: https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET
	RecoveryTarget *RecoveryTarget `json:"recoveryTarget,omitempty"`
}

// BackupSource contains the backup we need to restore from, plus some
// information that could be needed to correctly restore it.
type BackupSource struct {
	LocalObjectReference `json:",inline"`
	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive.
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`
}

// BootstrapPgBaseBackup contains the configuration required to take
// a physical backup of an existing PostgreSQL cluster
type BootstrapPgBaseBackup struct {
	// The name of the server of which we need to take a physical backup
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source"`
}

// RecoveryTarget allows to configure the moment where the recovery process
// will stop. All the target options except TargetTLI are mutually exclusive.
type RecoveryTarget struct {
	// The target timeline ("latest" or a positive integer)
	TargetTLI string `json:"targetTLI,omitempty"`

	// The target transaction ID
	TargetXID string `json:"targetXID,omitempty"`

	// The target name (to be previously created
	// with `pg_create_restore_point`)
	TargetName string `json:"targetName,omitempty"`

	// The target LSN (Log Sequence Number)
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
	// +kubebuilder:default:=true
	ResizeInUseVolumes *bool `json:"resizeInUseVolumes,omitempty"`

	// Template to be used to generate the Persistent Volume Claim
	// +optional
	PersistentVolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"pvcTemplate,omitempty"`
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
	TopologyKey string `json:"topologyKey"`

	// NodeSelector is map of key-value pairs used to define the nodes on which
	// the pods can run.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

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
	StartedAt metav1.Time `json:"startedAt,omitempty"`
}

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

// BarmanObjectStoreConfiguration contains the backup configuration
// using Barman against an S3-compatible object storage
type BarmanObjectStoreConfiguration struct {
	// The credentials to use to upload data to S3
	S3Credentials *S3Credentials `json:"s3Credentials,omitempty"`

	// The credentials to use to upload data to Azure Blob Storage
	AzureCredentials *AzureCredentials `json:"azureCredentials,omitempty"`

	// The credentials to use to upload data to Google Cloud Storage
	GoogleCredentials *GoogleCredentials `json:"googleCredentials,omitempty"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	EndpointURL string `json:"endpointURL,omitempty"`

	// EndpointCA store the CA bundle of the barman endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and barman-cloud-wal-archive
	EndpointCA *SecretKeySelector `json:"endpointCA,omitempty"`

	// The path where to store the backup (i.e. s3://bucket/path/to/folder)
	// this path, with different destination folders, will be used for WALs
	// and for data
	// +kubebuilder:validation:MinLength=1
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

	// Tags is a list of key value pairs that will be passed to the
	// Barman --tags option.
	Tags map[string]string `json:"tags,omitempty"`

	// HistoryTags is a list of key value pairs that will be passed to the
	// Barman --history-tags option.
	HistoryTags map[string]string `json:"historyTags,omitempty"`
}

// BackupConfiguration defines how the backup of the cluster are taken.
// Currently the only supported backup method is barmanObjectStore.
// For details and examples refer to the Backup and Recovery section of the
// documentation
type BackupConfiguration struct {
	// The configuration for the barman-cloud tool suite
	BarmanObjectStore *BarmanObjectStoreConfiguration `json:"barmanObjectStore,omitempty"`

	// RetentionPolicy is the retention policy to be used for backups
	// and WALs (i.e. '60d'). The retention policy is expressed in the form
	// of `XXu` where `XX` is a positive integer and `u` is in `[dwm]` -
	// days, weeks, months.
	// +kubebuilder:validation:Pattern=^[1-9][0-9]*[dwm]$
	// +optional
	RetentionPolicy string `json:"retentionPolicy,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Compress a WAL file before sending it to the object store. Available
	// options are empty string (no compression, default), `gzip`, `bzip2` or `snappy`.
	// +kubebuilder:validation:Enum=gzip;bzip2;snappy
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Number of WAL files to be either archived in parallel (when the
	// PostgreSQL instance is archiving to a backup object store) or
	// restored in parallel (when a PostgreSQL standby is fetching WAL
	// files from a recovery object store). If not specified, WAL files
	// will be processed one at a time. It accepts a positive integer as a
	// value - with 1 being the minimum accepted value.
	// +kubebuilder:validation:Minimum=1
	MaxParallel int `json:"maxParallel,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// Compress a backup file (a tar file per tablespace) while streaming it
	// to the object store. Available options are empty string (no
	// compression, default), `gzip`, `bzip2` or `snappy`.
	// +kubebuilder:validation:Enum=gzip;bzip2;snappy
	Compression CompressionType `json:"compression,omitempty"`

	// Whenever to force the encryption of files (if the bucket is
	// not already configured for that).
	// Allowed options are empty string (use the bucket policy, default),
	// `AES256` and `aws:kms`
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	Encryption EncryptionType `json:"encryption,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`

	// The number of parallel jobs to be used to upload the backup, defaults
	// to 2
	// +kubebuilder:validation:Minimum=1
	Jobs *int32 `json:"jobs,omitempty"`
}

// S3Credentials is the type for the credentials to be used to upload
// files to S3. It can be provided in two alternative ways:
//
// - explicitly passing accessKeyId and secretAccessKey
//
// - inheriting the role from the pod environment by setting inheritFromIAMRole to true
type S3Credentials struct {
	// The reference to the access key id
	AccessKeyIDReference *SecretKeySelector `json:"accessKeyId,omitempty"`

	// The reference to the secret access key
	SecretAccessKeyReference *SecretKeySelector `json:"secretAccessKey,omitempty"`

	// The references to the session key
	SessionToken *SecretKeySelector `json:"sessionToken,omitempty"`

	// Use the role based authentication without providing explicitly the keys.
	// +optional
	InheritFromIAMRole bool `json:"inheritFromIAMRole"`
}

// AzureCredentials is the type for the credentials to be used to upload
// files to Azure Blob Storage. The connection string contains every needed
// information. If the connection string is not specified, we'll need the
// storage account name and also one (and only one) of:
//
// - storageKey
// - storageSasToken
type AzureCredentials struct {
	// The connection string to be used
	ConnectionString *SecretKeySelector `json:"connectionString,omitempty"`

	// The storage account where to upload data
	StorageAccount *SecretKeySelector `json:"storageAccount,omitempty"`

	// The storage account key to be used in conjunction
	// with the storage account name
	StorageKey *SecretKeySelector `json:"storageKey,omitempty"`

	// A shared-access-signature to be used in conjunction with
	// the storage account name
	StorageSasToken *SecretKeySelector `json:"storageSasToken,omitempty"`
}

// GoogleCredentials is the type for the Google Cloud Storage credentials.
// This needs to be specified even if we run inside a GKE environment.
type GoogleCredentials struct {
	// If set to true, will presume that it's running inside a GKE environment,
	// default to false.
	// +optional
	GKEEnvironment bool `json:"gkeEnvironment"`

	// The secret containing the Google Cloud Storage JSON file with the credentials
	ApplicationCredentials *SecretKeySelector `json:"applicationCredentials,omitempty"`
}

// MonitoringConfiguration is the type containing all the monitoring
// configuration for a certain cluster
type MonitoringConfiguration struct {
	// Whether the default queries should be injected.
	// Set it to `true` if you don't want to inject default queries into the cluster.
	// Default: false.
	// +kubebuilder:default:=false
	DisableDefaultQueries *bool `json:"disableDefaultQueries,omitempty"`

	// The list of config maps containing the custom queries
	CustomQueriesConfigMap []ConfigMapKeySelector `json:"customQueriesConfigMap,omitempty"`

	// The list of secrets containing the custom queries
	CustomQueriesSecret []SecretKeySelector `json:"customQueriesSecret,omitempty"`

	// Enable or disable the `PodMonitor`
	// +kubebuilder:default:=false
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
	ConnectionParameters map[string]string `json:"connectionParameters,omitempty"`

	// The reference to an SSL certificate to be used to connect to this
	// instance
	SSLCert *corev1.SecretKeySelector `json:"sslCert,omitempty"`

	// The reference to an SSL private key to be used to connect to this
	// instance
	SSLKey *corev1.SecretKeySelector `json:"sslKey,omitempty"`

	// The reference to an SSL CA public key to be used to connect to this
	// instance
	SSLRootCert *corev1.SecretKeySelector `json:"sslRootCert,omitempty"`

	// The reference to the password to be used to connect to the server
	Password *corev1.SecretKeySelector `json:"password,omitempty"`

	// The configuration for the barman-cloud tool suite
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
	SuperuserSecretVersion string `json:"superuserSecretVersion,omitempty"`

	// The resource version of the "streaming_replica" user secret
	ReplicationSecretVersion string `json:"replicationSecretVersion,omitempty"`

	// The resource version of the "app" user secret
	ApplicationSecretVersion string `json:"applicationSecretVersion,omitempty"`

	// Unused. Retained for compatibility with old versions.
	CASecretVersion string `json:"caSecretVersion,omitempty"`

	// The resource version of the PostgreSQL client-side CA secret version
	ClientCASecretVersion string `json:"clientCaSecretVersion,omitempty"`

	// The resource version of the PostgreSQL server-side CA secret version
	ServerCASecretVersion string `json:"serverCaSecretVersion,omitempty"`

	// The resource version of the PostgreSQL server-side secret version
	ServerSecretVersion string `json:"serverSecretVersion,omitempty"`

	// The resource version of the Barman Endpoint CA if provided
	BarmanEndpointCA string `json:"barmanEndpointCA,omitempty"`

	// A map with the versions of all the secrets used to pass metrics.
	// Map keys are the secret names, map values are the versions
	Metrics map[string]string `json:"metrics,omitempty"`
}

// ConfigMapResourceVersion is the resource versions of the secrets
// managed by the operator
type ConfigMapResourceVersion struct {
	// A map with the versions of all the config maps used to pass metrics.
	// Map keys are the config map names, map values are the versions
	Metrics map[string]string `json:"metrics,omitempty"`
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

// GetEnableSuperuserAccess returns if the superuser access is enabled or not
func (cluster *Cluster) GetEnableSuperuserAccess() bool {
	if cluster.Spec.EnableSuperuserAccess != nil {
		return *cluster.Spec.EnableSuperuserAccess
	}

	return true
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
	return 30
}

// GetMaxStopDelay get the amount of time PostgreSQL has to stop
func (cluster *Cluster) GetMaxStopDelay() int32 {
	if cluster.Spec.MaxStopDelay > 0 {
		return cluster.Spec.MaxStopDelay
	}
	return 30
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
// defaulting to switchover
func (cluster *Cluster) GetPrimaryUpdateMethod() PrimaryUpdateMethod {
	strategy := cluster.Spec.PrimaryUpdateMethod
	if strategy == "" {
		return PrimaryUpdateMethodSwitchover
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

// IsBarmanBackupConfigured returns true if one of the possible backup destination
// is configured, false otherwise
func (backupConfiguration *BackupConfiguration) IsBarmanBackupConfigured() bool {
	return backupConfiguration != nil && backupConfiguration.BarmanObjectStore != nil &&
		(backupConfiguration.BarmanObjectStore.AzureCredentials != nil ||
			backupConfiguration.BarmanObjectStore.S3Credentials != nil ||
			backupConfiguration.BarmanObjectStore.GoogleCredentials != nil)
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
