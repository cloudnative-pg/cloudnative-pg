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

// Package configuration contains the configuration of the operator, reading
// if from environment variables and from the ConfigMap
package configuration

import (
	"errors"
	"path"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/configparser"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

var (
	configurationLog = log.WithName("configuration")
	// ErrNamespaceEmpty is raised when the namespace is empty in namespaced deployment
	ErrNamespaceEmpty = errors.New(
		"NAMESPACED mode requires both OPERATOR_NAMESPACE and WATCH_NAMESPACE to be set to the same non-empty value. " +
			"Please ensure both environment variables are configured correctly")
	// ErrNamespaceMismatch is raised when the OperatorNamespace and WatchNamespace is not equal in namespaced deployment
	ErrNamespaceMismatch = errors.New(
		"NAMESPACED mode requires OPERATOR_NAMESPACE and WATCH_NAMESPACE to be equal. " +
			"Currently they are set to different values. " +
			"Please set both to the same namespace where the operator is deployed")
)

const (
	// DefaultOperatorPullSecretName is implicitly copied into newly created clusters.
	DefaultOperatorPullSecretName = "cnpg-pull-secret" // #nosec

	// CertificateDuration is the default value for the lifetime of the generated certificates
	CertificateDuration = 90

	// ExpiringCheckThreshold is the default threshold to consider a certificate as expiring
	ExpiringCheckThreshold = 7

	// DefaultKubernetesClusterDomain is the default value used as
	// Kubernetes cluster domain.
	DefaultKubernetesClusterDomain = "cluster.local"
)

// DefaultDrainTaints is the default list of taints the operator will watch and treat
// as Unschedule
var DefaultDrainTaints = []string{
	// Kubernetes well-known unschedulable taint
	// See: https://kubernetes.io/docs/reference/labels-annotations-taints/#node-kubernetes-io-unschedulable
	"node.kubernetes.io/unschedulable",

	// Used by the Kubernetes Cluster Autoscaler
	// nolint: lll
	// See: https://github.com/kubernetes/autoscaler/blob/aa1d413ea3bf319b56c7b2e65ade1a028e149439/cluster-autoscaler/cloudprovider/oci/nodepools/consts/annotations.go#L27
	"ToBeDeletedByClusterAutoscaler",

	// Used by Karpenter termination controller
	// See: https://karpenter.sh/docs/concepts/disruption/#termination-controller
	"karpenter.sh/disrupted",

	// Used by Karpenter disruption controller
	// See: https://karpenter.sh/v0.32/concepts/disruption/#disruption-controller
	"karpenter.sh/disruption",
}

// DefaultPluginSocketDir is the default directory where the plugin sockets are located.
const DefaultPluginSocketDir = "/plugins"

// Data is the struct containing the configuration of the operator.
// Usually the operator code will use the "Current" configuration.
type Data struct {
	// WebhookCertDir is the directory where the certificates for the webhooks
	// need to written. This is different between plain Kubernetes and OpenShift
	WebhookCertDir string `json:"webhookCertDir" env:"WEBHOOK_CERT_DIR"`

	// MetricsCertDir is the directory where the certificates for the metrics
	// server are stored. If set, the metrics server will use TLS.
	MetricsCertDir string `json:"metricsCertDir" env:"METRICS_CERT_DIR"`

	// PluginSocketDir is the directory where the plugins sockets are to be
	// found
	PluginSocketDir string `json:"pluginSocketDir" env:"PLUGIN_SOCKET_DIR"`

	// WatchNamespace is the namespace where the operator should watch and
	// is configurable via environment variables in the OpenShift console.
	// Multiple namespaces can be specified separated by comma
	WatchNamespace string `json:"watchNamespace" env:"WATCH_NAMESPACE"`

	// OperatorNamespace is the namespace where the operator is installed
	OperatorNamespace string `json:"operatorNamespace" env:"OPERATOR_NAMESPACE"`

	// Namespaced defines if the operator should only access and listen to resources within
	// its own namespace. Default false
	Namespaced bool `json:"namespaced" env:"NAMESPACED"`

	// OperatorPullSecretName is the pull secret used to download the
	// pull secret name
	OperatorPullSecretName string `json:"operatorPullSecretName" env:"PULL_SECRET_NAME"`

	// OperatorImageName is the name of the image of the operator, that is
	// used to bootstrap Pods
	OperatorImageName string `json:"operatorImageName" env:"OPERATOR_IMAGE_NAME"`

	// PostgresImageName is the name of the image of PostgreSQL that is
	// used by default for new clusters
	PostgresImageName string `json:"postgresImageName" env:"POSTGRES_IMAGE_NAME"`

	// PgbouncerImageName is the name of the image of PgBouncer that is
	// used by default for new poolers
	PgbouncerImageName string `json:"pgbouncerImageName" env:"PGBOUNCER_IMAGE_NAME"`

	// InheritedAnnotations is a list of annotations that every resource could inherit from
	// the owning Cluster
	InheritedAnnotations []string `json:"inheritedAnnotations" env:"INHERITED_ANNOTATIONS"`

	// InheritedLabels is a list of labels that every resource could inherit from
	// the owning Cluster
	InheritedLabels []string `json:"inheritedLabels" env:"INHERITED_LABELS"`

	// MonitoringQueriesConfigmap is the name of the configmap in the operator namespace which contain
	// the monitoring queries. The queries will be read from the data key: "queries".
	MonitoringQueriesConfigmap string `json:"monitoringQueriesConfigmap" env:"MONITORING_QUERIES_CONFIGMAP"`

	// MonitoringQueriesSecret is the name of the secret in the operator namespace which contain
	// the monitoring queries. The queries will be read from the data key: "queries".
	MonitoringQueriesSecret string `json:"monitoringQueriesSecret" env:"MONITORING_QUERIES_SECRET"`

	// EnableInstanceManagerInplaceUpdates enables the instance manager to apply in-place updates,
	// replacing the executable in a pod without restarting
	EnableInstanceManagerInplaceUpdates bool `json:"enableInstanceManagerInplaceUpdates" env:"ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES"` //nolint

	// This is the lifetime of the generated certificates
	CertificateDuration int `json:"certificateDuration" env:"CERTIFICATE_DURATION"`

	// Threshold to consider a certificate as expiring
	ExpiringCheckThreshold int `json:"expiringCheckThreshold" env:"EXPIRING_CHECK_THRESHOLD"`

	// CreateAnyService is true when the user wants the operator to create
	// the <cluster-name>-any service. Defaults to false.
	CreateAnyService bool `json:"createAnyService" env:"CREATE_ANY_SERVICE"`

	// The duration (in seconds) to wait between the roll-outs of different
	// clusters during an operator upgrade. This setting controls the
	// timing of upgrades across clusters, spreading them out to reduce
	// system impact. The default value is 0, which means no delay between
	// PostgreSQL cluster upgrades.
	ClustersRolloutDelay int `json:"clustersRolloutDelay" env:"CLUSTERS_ROLLOUT_DELAY"`

	// The duration (in seconds) to wait between roll-outs of individual
	// PostgreSQL instances within the same cluster during an operator
	// upgrade. The default value is 0, meaning no delay between upgrades
	// of instances in the same PostgreSQL cluster.
	InstancesRolloutDelay int `json:"instancesRolloutDelay" env:"INSTANCES_ROLLOUT_DELAY"`

	// IncludePlugins is a comma-separated list of plugins to always be
	// included in the Cluster reconciliation
	IncludePlugins string `json:"includePlugins" env:"INCLUDE_PLUGINS"`

	// StandbyTCPUserTimeout configuration parameter allows you to
	// specify a custom TCP user timeout for the standby PostgreSQL
	// server's connection to the primary server. This timeout is
	// added as a tcp_user_timeout option to the primary_conninfo
	// string, which is used by the standby server to connect to the
	// primary server in CloudNativePG.
	// When nil, the instance manager will use a default value of 5000ms.
	// Set to 0 explicitly to use the system's default.
	StandbyTCPUserTimeout *int `json:"standbyTcpUserTimeout" env:"STANDBY_TCP_USER_TIMEOUT"`

	// KubernetesClusterDomain defines the domain suffix for service FQDNs
	// within the Kubernetes cluster. If left unset, it defaults to `cluster.local`.
	KubernetesClusterDomain string `json:"kubernetesClusterDomain" env:"KUBERNETES_CLUSTER_DOMAIN"`

	// DrainTaints is a list of taints the operator will watch and treat as Unschedule
	DrainTaints []string `json:"drainTaints" env:"DRAIN_TAINTS"`
}

// Current is the configuration used by the operator
var Current = NewConfiguration()

// newDefaultConfig creates a configuration holding the defaults
func newDefaultConfig() *Data {
	return &Data{
		OperatorPullSecretName:  DefaultOperatorPullSecretName,
		OperatorImageName:       versions.DefaultOperatorImageName,
		PostgresImageName:       versions.DefaultImageName,
		PgbouncerImageName:      versions.DefaultPgbouncerImage,
		PluginSocketDir:         DefaultPluginSocketDir,
		CreateAnyService:        false,
		CertificateDuration:     CertificateDuration,
		ExpiringCheckThreshold:  ExpiringCheckThreshold,
		StandbyTCPUserTimeout:   nil,
		KubernetesClusterDomain: DefaultKubernetesClusterDomain,
		DrainTaints:             DefaultDrainTaints,
		Namespaced:              false,
	}
}

// NewConfiguration create a new CNPG configuration by reading
// the environment variables
func NewConfiguration() *Data {
	configuration := newDefaultConfig()
	configuration.ReadConfigMap(nil)
	return configuration
}

// ReadConfigMap reads the configuration from the environment and the passed in data map
func (config *Data) ReadConfigMap(data map[string]string) {
	configparser.ReadConfigMap(config, newDefaultConfig(), data)
}

// IsAnnotationInherited checks if an annotation with a certain name should
// be inherited from the Cluster specification to the generated objects
func (config *Data) IsAnnotationInherited(name string) bool {
	return evaluateGlobPatterns(config.InheritedAnnotations, name)
}

// IsLabelInherited checks if a label with a certain name should
// be inherited from the Cluster specification to the generated objects
func (config *Data) IsLabelInherited(name string) bool {
	return evaluateGlobPatterns(config.InheritedLabels, name)
}

// GetClustersRolloutDelay gets the delay between roll-outs of different clusters
func (config *Data) GetClustersRolloutDelay() time.Duration {
	return time.Duration(config.ClustersRolloutDelay) * time.Second
}

// GetInstancesRolloutDelay gets the delay between roll-outs of pods belonging
// to the same cluster
func (config *Data) GetInstancesRolloutDelay() time.Duration {
	return time.Duration(config.InstancesRolloutDelay) * time.Second
}

// WatchedNamespaces get the list of additional watched namespaces.
// The result is a list of namespaces specified in the WATCHED_NAMESPACE where
// each namespace is separated by comma
func (config *Data) WatchedNamespaces() []string {
	return cleanNamespaceList(config.WatchNamespace)
}

// GetIncludePlugins gets the list of plugins to be always
// included in the operator reconciliation
func (config *Data) GetIncludePlugins() []string {
	rawList := strings.Split(config.IncludePlugins, ",")
	result := make([]string, 0, len(rawList))
	for _, pluginName := range rawList {
		if trimmedPluginName := strings.TrimSpace(pluginName); trimmedPluginName != "" {
			result = append(result, trimmedPluginName)
		}
	}

	return result
}

// Validate validates configuration parameters and combinations.
// This can programatically validate deployment parameters
// If validation fails it returns an error.
func (config *Data) Validate() error {
	if err := config.validateNamespacedConfiguration(); err != nil {
		return err
	}

	return nil
}

// ValidateNamespacedConfiguration validates that when namespaced mode is enabled,
// the operator namespace and watch namespace must be equal and non-empty
func (config *Data) validateNamespacedConfiguration() error {
	if !config.Namespaced {
		return nil
	}

	if config.OperatorNamespace == "" {
		return ErrNamespaceEmpty
	}

	if config.WatchNamespace == "" {
		return ErrNamespaceEmpty
	}

	if config.OperatorNamespace != config.WatchNamespace {
		return ErrNamespaceMismatch
	}

	return nil
}

func cleanNamespaceList(namespaces string) (result []string) {
	unfilteredList := strings.Split(namespaces, ",")
	result = make([]string, 0, len(unfilteredList))

	for _, elem := range unfilteredList {
		elem = strings.TrimSpace(elem)
		if len(elem) != 0 {
			result = append(result, elem)
		}
	}

	return result
}

func evaluateGlobPatterns(patterns []string, value string) (result bool) {
	var err error

	for _, pattern := range patterns {
		if result, err = path.Match(pattern, value); err != nil {
			configurationLog.Info(
				"Skipping invalid glob pattern during labels/annotations inheritance",
				"pattern", pattern)
			continue
		}

		if result {
			return result
		}
	}

	return result
}
