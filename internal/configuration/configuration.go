/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package configuration contains the configuration of the operator, reading
// if from environment variables and from the ConfigMap
package configuration

import (
	"os"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// DefaultOperatorPullSecretName is implicitly copied into newly created clusters.
const DefaultOperatorPullSecretName = "postgresql-operator-pull-secret" // #nosec

var (
	// webhookCertDir is the directory where the certificates for the webhooks
	// need to written. This is different between plain Kubernetes and OpenShift
	webhookCertDir string

	// watchNamespace is the namespace where the operator should watch and
	// is configurable via environment variables of via the OpenShift console
	watchNamespace string

	// operatorNamespace is the namespace where the operator is installed
	operatorNamespace string

	// operatorPullSecretName is the pull secret used to download the
	// pull secret name
	operatorPullSecretName string

	// operatorImageName is the name of the image of the operator, that is
	// used to bootstrap Pods
	operatorImageName string

	// postgresImageName is the name of the image of PostgreSQL that is
	// used by default for new clusters
	postgresImageName string
)

func init() {
	webhookCertDir = os.Getenv("WEBHOOK_CERT_DIR")
	watchNamespace = os.Getenv("WATCH_NAMESPACE")
	operatorNamespace = os.Getenv("OPERATOR_NAMESPACE")

	operatorPullSecretName = os.Getenv("PULL_SECRET_NAME")
	if operatorPullSecretName == "" {
		operatorPullSecretName = DefaultOperatorPullSecretName
	}

	operatorImageName = os.Getenv("OPERATOR_IMAGE_NAME")
	if operatorImageName == "" {
		operatorImageName = versions.DefaultOperatorImageName
	}

	postgresImageName = os.Getenv("POSTGRES_IMAGE_NAME")
	if postgresImageName == "" {
		postgresImageName = versions.DefaultImageName
	}
}

// GetDefaultPostgresImageName gets the default image that is used
// for PostgreSQL in this operator
func GetDefaultPostgresImageName() string {
	return postgresImageName
}

// GetOperatorImageName gets the operator image name that will be used
func GetOperatorImageName() string {
	return operatorImageName
}

// GetOperatorNamespace gets the namespace under which the operator is deployed
func GetOperatorNamespace() string {
	return operatorNamespace
}

// GetOperatorPullSecretName gets the pull secret that is being used to
// download the operator image
func GetOperatorPullSecretName() string {
	return operatorPullSecretName
}

// GetWebhookCertDir gets the directory where the webhooks certificate
// need to be found
func GetWebhookCertDir() string {
	return webhookCertDir
}

// GetWatchNamespace gets the namespace that will be watched by the operator
func GetWatchNamespace() string {
	return watchNamespace
}

// ReadConfigMap reads the configuration from the operator ConfigMap
func ReadConfigMap(data map[string]string) {
	if pullSecretName, ok := data["PULL_SECRET_NAME"]; ok {
		operatorPullSecretName = pullSecretName
	}
}
