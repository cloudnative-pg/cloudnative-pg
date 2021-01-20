/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// buildPrimaryConnInfo builds the connection string to connect to primaryHostname
func buildPrimaryConnInfo(primaryHostname, applicationName string) string {
	primaryConnInfo := fmt.Sprintf("host=%v ", primaryHostname) +
		fmt.Sprintf("user=%v ", v1alpha1.StreamingReplicationUser) +
		"port=5432 " +
		fmt.Sprintf("sslkey=%v ", postgres.StreamingReplicaKeyLocation) +
		fmt.Sprintf("sslcert=%v ", postgres.StreamingReplicaCertificateLocation) +
		fmt.Sprintf("sslrootcert=%v ", postgres.CACertificateLocation) +
		fmt.Sprintf("application_name=%v ", applicationName) +
		"sslmode=require"
	return primaryConnInfo
}
