/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package versions contains the version the Cloud Native PostgreSQL
// operator and the software used by it
package versions

const (
	// Version is the version of the operator
	Version = "1.2.0"

	// DefaultImageName is the image used by default by the operator to create
	// pods.
	DefaultImageName = "quay.io/enterprisedb/postgresql:13.2"

	// DefaultOperatorImageName used to bootstrap the controller in the Pods running
	// PostgreSQL
	DefaultOperatorImageName = "quay.io/enterprisedb/cloud-native-postgresql:1.2.0"
)
