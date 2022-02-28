/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/configfile"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// buildPrimaryConnInfo builds the connection string to connect to primaryHostname
func buildPrimaryConnInfo(primaryHostname, applicationName string) string {
	primaryConnInfoParameters := map[string]string{
		"host":             primaryHostname,
		"user":             apiv1.StreamingReplicationUser,
		"port":             fmt.Sprintf("%d", GetServerPort()),
		"sslkey":           postgres.StreamingReplicaKeyLocation,
		"sslcert":          postgres.StreamingReplicaCertificateLocation,
		"sslrootcert":      postgres.ServerCACertificateLocation,
		"application_name": applicationName,
		"sslmode":          "verify-ca",
	}
	return configfile.CreateConnectionString(primaryConnInfoParameters)
}
