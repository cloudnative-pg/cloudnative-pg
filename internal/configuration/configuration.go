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

// Package configuration contains the configuration of the operator, reading
// if from environment variables and from the ConfigMap
package configuration

import (
	"path"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/configparser"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

var configurationLog = log.WithName("configuration")

// DefaultOperatorPullSecretName is implicitly copied into newly created clusters.
const DefaultOperatorPullSecretName = "cnpg-pull-secret" // #nosec

// Data is the struct containing the configuration of the operator.
// Usually the operator code will use the "Current" configuration.
type Data struct {
	// WebhookCertDir is the directory where the certificates for the webhooks
	// need to written. This is different between plain Kubernetes and OpenShift
	WebhookCertDir string `json:"webhookCertDir" env:"WEBHOOK_CERT_DIR"`

	// WatchNamespace is the namespace where the operator should watch and
	// is configurable via environment variables in the OpenShift console.
	// Multiple namespaces can be specified separated by comma
	WatchNamespace string `json:"watchNamespace" env:"WATCH_NAMESPACE"`

	// EnablePodDebugging enable debugging mode in new generated pods
	EnablePodDebugging bool `json:"enablePodDebugging" env:"POD_DEBUG"`

	// OperatorNamespace is the namespace where the operator is installed
	OperatorNamespace string `json:"operatorNamespace" env:"OPERATOR_NAMESPACE"`

	// OperatorPullSecretName is the pull secret used to download the
	// pull secret name
	OperatorPullSecretName string `json:"operatorPullSecretName" env:"PULL_SECRET_NAME"`

	// OperatorImageName is the name of the image of the operator, that is
	// used to bootstrap Pods
	OperatorImageName string `json:"operatorImageName" env:"OPERATOR_IMAGE_NAME"`

	// PostgresImageName is the name of the image of PostgreSQL that is
	// used by default for new clusters
	PostgresImageName string `json:"postgresImageName" env:"POSTGRES_IMAGE_NAME"`

	// InheritedAnnotations is a list of annotations that every resource could inherit from
	// the owning Cluster
	InheritedAnnotations []string `json:"inheritedAnnotations" env:"INHERITED_ANNOTATIONS"`

	// InheritedLabels is a list of labels that every resource could inherit from
	// the owning Cluster
	InheritedLabels []string `json:"inheritedLabels" env:"INHERITED_LABELS"`

	// EnableInstanceManagerInplaceUpdates enables the instance manager to apply in-place updates,
	// replacing the executable in a pod without restarting
	EnableInstanceManagerInplaceUpdates bool `json:"enableInstanceManagerInplaceUpdates" env:"ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES"` //nolint

	// EnableAzurePVCUpdates enables the live update of PVC in Azure environment
	EnableAzurePVCUpdates bool `json:"enableAzurePVCUpdates" env:"ENABLE_AZURE_PVC_UPDATES"`

	// MonitoringQueriesConfigmap is the name of the configmap in the operator namespace which contain
	// the monitoring queries. The queries will be read from the data key: "queries".
	MonitoringQueriesConfigmap string `json:"monitoringQueriesConfigmap" env:"MONITORING_QUERIES_CONFIGMAP"`

	// MonitoringQueriesSecret is the name of the secret in the operator namespace which contain
	// the monitoring queries. The queries will be read from the data key: "queries".
	MonitoringQueriesSecret string `json:"monitoringQueriesSecret" env:"MONITORING_QUERIES_SECRET"`

	// CreateAnyService is true when the user wants the operator to create
	// the <cluster-name>-any service. Defaults to false.
	CreateAnyService bool `json:"createAnyService" env:"CREATE_ANY_SERVICE"`
}

// Current is the configuration used by the operator
var Current = NewConfiguration()

// newDefaultConfig creates a configuration holding the defaults
func newDefaultConfig() *Data {
	return &Data{
		OperatorPullSecretName: DefaultOperatorPullSecretName,
		OperatorImageName:      versions.DefaultOperatorImageName,
		PostgresImageName:      versions.DefaultImageName,
		CreateAnyService:       false,
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
	configparser.ReadConfigMap(config, newDefaultConfig(), data, configparser.OsEnvironment{})
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

// WatchedNamespaces get the list of additional watched namespaces.
// The result is a list of namespaces specified in the WATCHED_NAMESPACE where
// each namespace is separated by comma
func (config *Data) WatchedNamespaces() []string {
	return cleanNamespaceList(config.WatchNamespace)
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

	return
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
			return
		}
	}

	return
}
