/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package versions contains the version the Cloud Native PostgreSQL
// operator and the software used by it
package versions

import "os"

const (
	// Version is the version of the operator
	Version = "1.0.0"

	// DefaultImageName is the image used by default by the operator to create
	// pods.
	DefaultImageName = "quay.io/enterprisedb/postgresql:13.1"

	// DefaultOperatorImageName used to bootstrap the controller in the Pods running
	// PostgreSQL
	DefaultOperatorImageName = "quay.io/enterprisedb/cloud-native-postgresql:1.0.0"

	// postgresImageNameEnvVar is the environment variable that allow overriding the default image used
	// for PostgreSQL
	postgresImageNameEnvVar = "POSTGRES_IMAGE_NAME"

	// operatorImageNameEnvVar is the environment variable that allow overriding the default image used
	// for the PostgreSQL operator
	operatorImageNameEnvVar = "OPERATOR_IMAGE_NAME"
)

// GetDefaultImageName gets the name of the image that will be used
// by the operator to create pods. This can be overridden using an
// environment variable.
func GetDefaultImageName() string {
	result := os.Getenv(postgresImageNameEnvVar)
	if len(result) == 0 {
		result = DefaultImageName
	}

	return result
}

// GetDefaultOperatorImageName gets the name of the image of the
// operator.
func GetDefaultOperatorImageName() string {
	result := os.Getenv(operatorImageNameEnvVar)
	if len(result) == 0 {
		return DefaultOperatorImageName
	}

	return result
}
