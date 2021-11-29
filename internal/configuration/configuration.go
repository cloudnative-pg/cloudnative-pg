/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package configuration contains the configuration of the operator, reading
// if from environment variables and from the ConfigMap
package configuration

import (
	"path"
	"strings"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configparser"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

var configurationLog = log.WithName("configuration")

// DefaultOperatorPullSecretName is implicitly copied into newly created clusters.
const DefaultOperatorPullSecretName = "postgresql-operator-pull-secret" // #nosec

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

	// EnableInstanceManagerInplaceUpdates enables the instance manager to apply inplace updates,
	// replacing the executable in a pod without restarting
	EnableInstanceManagerInplaceUpdates bool `json:"enableInstanceManagerInplaceUpdates" env:"ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES"` //nolint

	// EnableAzurePVCUpdates enables the live update of PVC in Azure environment
	EnableAzurePVCUpdates bool `json:"enableAzurePVCUpdates" env:"ENABLE_AZURE_PVC_UPDATES"`

	// MonitoringQueriesConfigmap is the name of the configmap in the operator namespace which contain
	// the monitoring queries. The queries will be read from the data key: "queries".
	MonitoringQueriesConfigmap string `json:"monitoringQueriesConfigmap" env:"MONITORING_QUERIES_CONFIGMAP"`
}

// Current is the configuration used by the operator
var Current = NewConfiguration()

// newDefaultConfig creates a configuration holding the defaults
func newDefaultConfig() *Data {
	return &Data{
		OperatorPullSecretName: DefaultOperatorPullSecretName,
		OperatorImageName:      versions.DefaultOperatorImageName,
		PostgresImageName:      versions.DefaultImageName,
	}
}

// NewConfiguration create a new CNP configuration by reading
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
