/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package versions

import "os"

const (
	// Version is the version of the operator
	Version = "0.0.1"

	// DefaultImageName is the image used by default by the operator to create
	// pods.
	DefaultImageName = "2ndq.io/release/k8s/postgresql:12.1"

	postgresImageNameEnvVar = "POSTGRES_IMAGE_NAME"
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
