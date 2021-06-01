/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package configuration contains the configuration of the operator, reading
// if from environment variables and from the ConfigMap
package configuration

import (
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

var log = ctrl.Log.WithName("configuration")

// DefaultOperatorPullSecretName is implicitly copied into newly created clusters.
const DefaultOperatorPullSecretName = "postgresql-operator-pull-secret" // #nosec

// Data is the struct containing the configuration of the operator.
// Usually the operator code will used the "Current" configuration.
type Data struct {
	// WebhookCertDir is the directory where the certificates for the webhooks
	// need to written. This is different between plain Kubernetes and OpenShift
	WebhookCertDir string `json:"webhookCertDir" env:"WEBHOOK_CERT_DIR"`

	// WatchNamespace is the namespace where the operator should watch and
	// is configurable via environment variables of via the OpenShift console
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
	config.readConfigMap(data, OsEnvironment{})
}

func (config *Data) readConfigMap(data map[string]string, env EnvironmentSource) {
	defaults := newDefaultConfig()
	count := reflect.TypeOf(Data{}).NumField()
	for i := 0; i < count; i++ {
		field := reflect.TypeOf(Data{}).Field(i)
		envName := field.Tag.Get("env")

		// Fields without env tag are skipped.
		if envName == "" {
			continue
		}

		// Initialize value with default
		value := reflect.ValueOf(defaults).Elem().FieldByName(field.Name).String()
		// If the key is present in the environment, use its value
		if envValue := env.Getenv(envName); envValue != "" {
			value = envValue
		}
		// If the key is present in the passed data, use its value
		if mapValue, ok := data[envName]; ok {
			value = mapValue
		}

		switch t := field.Type; t.Kind() {
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(value)
			if err != nil {
				log.Info(
					"Skipping invalid boolean value parsing configuration",
					"field", field.Name, "value", value)
				continue
			}
			reflect.ValueOf(config).Elem().FieldByName(field.Name).SetBool(boolValue)
		case reflect.String:
			reflect.ValueOf(config).Elem().FieldByName(field.Name).SetString(value)
		case reflect.Slice:
			reflect.ValueOf(config).Elem().FieldByName(field.Name).Set(reflect.ValueOf(splitAndTrim(value)))
		default:
			errMsg := fmt.Sprintf(
				"field: %s, type: %s, kind: %s is not being handled",
				field.Name, t.String(), t.Kind())
			panic(errMsg)
		}
	}
}

// splitAndTrim slices a string into all substrings after each comma and
// returns a slice of those space-trimmed substrings.
func splitAndTrim(commaSeparatedList string) []string {
	list := strings.Split(commaSeparatedList, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list
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

func evaluateGlobPatterns(patterns []string, value string) (result bool) {
	var err error

	for _, pattern := range patterns {
		if result, err = path.Match(pattern, value); err != nil {
			log.Info(
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
