/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package versions contains the version of the Cloud Native PostgreSQL operator and the software
// that is used by it
package versions

const (
	// Version is the version of the operator
	Version = "1.9.2"

	// DefaultImageName is the default image used by the operator to create pods
	DefaultImageName = "quay.io/enterprisedb/postgresql:14.0"

	// DefaultOperatorImageName is the default operator image used by the controller in the pods running PostgreSQL
	DefaultOperatorImageName = "quay.io/enterprisedb/cloud-native-postgresql:1.9.2"
)

// BuildInfo is a struct containing all the info about the build
type BuildInfo struct {
	Version, Commit, Date string
}

var (
	// buildVersion injected during the build
	buildVersion = "1.9.2"

	// buildCommit injected during the build
	buildCommit = "none"

	// buildDate injected during the build
	buildDate = "unknown"

	// Info contains the build info
	Info = BuildInfo{
		Version: buildVersion,
		Commit:  buildCommit,
		Date:    buildDate,
	}
)
