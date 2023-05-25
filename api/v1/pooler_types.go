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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PoolerType is the type of the connection pool, meaning the service
// we are targeting
// +kubebuilder:validation:Enum=rw;ro
type PoolerType string

const (
	// PoolerTypeRW means that the pooler involves only the primary server
	PoolerTypeRW = PoolerType("rw")

	// PoolerTypeRO means that the pooler involver every server
	PoolerTypeRO = PoolerType("ro")

	// DefaultPgBouncerPoolerAuthQuery is the default auth_query for PgBouncer
	DefaultPgBouncerPoolerAuthQuery = "SELECT usename, passwd FROM user_search($1)"
)

// PgBouncerPoolMode is the mode of PgBouncer
// +kubebuilder:validation:Enum=session;transaction
type PgBouncerPoolMode string

const (
	// PgBouncerPoolModeSession the "session" mode
	PgBouncerPoolModeSession = PgBouncerPoolMode("session")

	// PgBouncerPoolModeTransaction the "transaction" mode
	PgBouncerPoolModeTransaction = PgBouncerPoolMode("transaction")
)

// PoolerSpec defines the desired state of Pooler
type PoolerSpec struct {
	// This is the cluster reference on which the Pooler will work.
	// Pooler name should never match with any cluster name within the same namespace.
	Cluster LocalObjectReference `json:"cluster"`

	// Which instances we must forward traffic to?
	// +kubebuilder:default:=rw
	Type PoolerType `json:"type"`

	// The number of replicas we want
	// +kubebuilder:default:=1
	Instances int32 `json:"instances"`

	// The template of the Pod to be created
	Template *PodTemplateSpec `json:"template,omitempty"`

	// The PgBouncer configuration
	PgBouncer *PgBouncerSpec `json:"pgbouncer"`

	// The deployment strategy to use for pgbouncer to replace existing pods with new ones
	DeploymentStrategy *appsv1.DeploymentStrategy `json:"deploymentStrategy,omitempty"`

	// The configuration of the monitoring infrastructure of this pooler.
	Monitoring *PoolerMonitoringConfiguration `json:"monitoring,omitempty"`
}

// PoolerMonitoringConfiguration is the type containing all the monitoring
// configuration for a certain Pooler.
//
// Mirrors the Cluster's MonitoringConfiguration but without the custom queries
// part for now.
type PoolerMonitoringConfiguration struct {
	// Enable or disable the `PodMonitor`
	// +kubebuilder:default:=false
	EnablePodMonitor bool `json:"enablePodMonitor,omitempty"`
}

// PodTemplateSpec is a structure allowing the user to set
// a template for Pod generation.
//
// Unfortunately we can't use the corev1.PodTemplateSpec
// type because the generated CRD won't have the field for the
// metadata section.
//
// References:
// https://github.com/kubernetes-sigs/controller-tools/issues/385
// https://github.com/kubernetes-sigs/controller-tools/issues/448
// https://github.com/prometheus-operator/prometheus-operator/issues/3041
type PodTemplateSpec struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta Metadata `json:"metadata,omitempty"`

	// Specification of the desired behavior of the pod.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec corev1.PodSpec `json:"spec,omitempty"`
}

// PgBouncerSpec defines how to configure PgBouncer
type PgBouncerSpec struct {
	// The pool mode
	// +kubebuilder:default:=session
	PoolMode PgBouncerPoolMode `json:"poolMode"`

	// The credentials of the user that need to be used for the authentication
	// query. In case it is specified, also an AuthQuery
	// (e.g. "SELECT usename, passwd FROM pg_shadow WHERE usename=$1")
	// has to be specified and no automatic CNPG Cluster integration will be triggered.
	AuthQuerySecret *LocalObjectReference `json:"authQuerySecret,omitempty"`

	// The query that will be used to download the hash of the password
	// of a certain user. Default: "SELECT usename, passwd FROM user_search($1)".
	// In case it is specified, also an AuthQuerySecret has to be specified and
	// no automatic CNPG Cluster integration will be triggered.
	AuthQuery string `json:"authQuery,omitempty"`

	// Additional parameters to be passed to PgBouncer - please check
	// the CNPG documentation for a list of options you can configure
	Parameters map[string]string `json:"parameters,omitempty"`

	// PostgreSQL Host Based Authentication rules (lines to be appended
	// to the pg_hba.conf file)
	// +optional
	PgHBA []string `json:"pg_hba,omitempty"`

	// When set to `true`, PgBouncer will disconnect from the PostgreSQL
	// server, first waiting for all queries to complete, and pause all new
	// client connections until this value is set to `false` (default). Internally,
	// the operator calls PgBouncer's `PAUSE` and `RESUME` commands.
	// +kubebuilder:default:=false
	Paused *bool `json:"paused,omitempty"`
}

// IsPaused returns whether all database should be paused or not
func (in PgBouncerSpec) IsPaused() bool {
	return in.Paused != nil && *in.Paused
}

// PoolerStatus defines the observed state of Pooler
type PoolerStatus struct {
	// The resource version of the config object
	Secrets *PoolerSecrets `json:"secrets,omitempty"`
	// The number of pods trying to be scheduled
	Instances int32 `json:"instances,omitempty"`
}

// PoolerSecrets contains the versions of all the secrets used
type PoolerSecrets struct {
	// The server TLS secret version
	ServerTLS SecretVersion `json:"serverTLS,omitempty"`

	// The server CA secret version
	ServerCA SecretVersion `json:"serverCA,omitempty"`

	// The client CA secret version
	ClientCA SecretVersion `json:"clientCA,omitempty"`

	// The version of the secrets used by PgBouncer
	PgBouncerSecrets *PgBouncerSecrets `json:"pgBouncerSecrets,omitempty"`
}

// PgBouncerSecrets contains the versions of the secrets used
// by pgbouncer
type PgBouncerSecrets struct {
	// The auth query secret version
	AuthQuery SecretVersion `json:"authQuery,omitempty"`
}

// SecretVersion contains a secret name and its ResourceVersion
type SecretVersion struct {
	// The name of the secret
	Name string `json:"name,omitempty"`

	// The ResourceVersion of the secret
	Version string `json:"version,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.cluster.name"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:subresource:scale:specpath=.spec.instances,statuspath=.status.instances

// Pooler is the Schema for the poolers API
type Pooler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PoolerSpec   `json:"spec,omitempty"`
	Status PoolerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PoolerList contains a list of Pooler
type PoolerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Pooler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Pooler{}, &PoolerList{})
}

// GetAuthQuerySecretName returns the specified AuthQuerySecret name for PgBouncer
// if provided or the default name otherwise.
func (in *Pooler) GetAuthQuerySecretName() string {
	if in.Spec.PgBouncer.AuthQuerySecret != nil {
		return in.Spec.PgBouncer.AuthQuerySecret.Name
	}

	return in.Spec.Cluster.Name + DefaultPgBouncerPoolerSecretSuffix
}

// GetAuthQuery returns the specified AuthQuery name for PgBouncer
// if provided or the default name otherwise.
func (in *Pooler) GetAuthQuery() string {
	if in.Spec.PgBouncer.AuthQuery != "" {
		return in.Spec.PgBouncer.AuthQuery
	}

	return DefaultPgBouncerPoolerAuthQuery
}
