/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// buildPrimaryConnInfo builds the connection string to connect to primaryHostname
func buildPrimaryConnInfo(primaryHostname, applicationName string) string {
	primaryConnInfo := fmt.Sprintf("host=%v ", primaryHostname) +
		fmt.Sprintf("user=%v ", apiv1.StreamingReplicationUser) +
		fmt.Sprintf("port=%v ", GetServerPort()) +
		fmt.Sprintf("sslkey=%v ", postgres.StreamingReplicaKeyLocation) +
		fmt.Sprintf("sslcert=%v ", postgres.StreamingReplicaCertificateLocation) +
		fmt.Sprintf("sslrootcert=%v ", postgres.ServerCACertificateLocation) +
		fmt.Sprintf("application_name=%v ", applicationName) +
		"sslmode=verify-ca"
	return primaryConnInfo
}
