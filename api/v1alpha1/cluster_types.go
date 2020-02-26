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

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/versions"
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

	// Secret for pulling the image. If empty
	// no secret will be used
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// Number of instances required in the cluster
	// +kubebuilder:validation:Minimum=1
	Instances int32 `json:"instances"`

	// Configuration of the PostgreSQL server
	PostgresConfiguration PostgresConfiguration `json:"postgresql"`

	// Configuration from the application point of view
	ApplicationConfiguration ApplicationConfiguration `json:"applicationConfiguration"`

	// Configuration of the storage of the instances
	// +optional
	StorageConfiguration *StorageConfiguration `json:"storage,omitempty"`

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
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// Name of the image used
	ImageName string `json:"imageName,omitempty"`

	// Total number of instances in the cluster
	Instances int32 `json:"instances,omitempty"`

	// Total number of ready instances in the cluster
	ReadyInstances int32 `json:"readyInstances,omitempty"`

	// Current primary instance
	CurrentPrimary string `json:"currentPrimary,omitempty"`

	// Target primary instance, this is different from the previous one
	// during a switchover or a failover
	TargetPrimary string `json:"targetPrimary,omitempty"`

	// ID of the latest generated node (used to avoid node name clashing)
	LatestGeneratedNode int32 `json:"latestGeneratedNode,omitempty"`
}

// PostgresConfiguration defines the PostgreSQL configuration
type PostgresConfiguration struct {
	// PostgreSQL configuration options (postgresql.conf)
	Parameters []string `json:"parameters"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	PgHBA []string `json:"pg_hba"`
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
	// +optional
	StorageClass *string `json:"storageClass"`

	// Size of the storage. Required if not already specified in the PVC template.
	// +optional
	Size *resource.Quantity `json:"size"`

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

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

// GetImageName get the name of the image that should be used
// to create the pods
func (cluster *Cluster) GetImageName() string {
	if len(cluster.Status.ImageName) > 0 {
		return cluster.Status.ImageName
	}

	if len(cluster.Spec.ImageName) > 0 {
		return cluster.Spec.ImageName
	}

	return versions.GetDefaultImageName()
}

// GetImagePullSecret get the name of the pull secret to use
// to download the PostgreSQL image
func (cluster *Cluster) GetImagePullSecret() string {
	return cluster.Spec.ImagePullSecret
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

// IsUsingPersistentStorage check if this cluster will use persistent storage
// or not
func (cluster *Cluster) IsUsingPersistentStorage() bool {
	return cluster.Spec.StorageConfiguration != nil
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

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
