/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"fmt"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/postgres"
)

// buildPrimaryConnInfo builds the connection string to connect to primaryHostname
func buildPrimaryConnInfo(primaryHostname string) string {
	primaryConnInfo := fmt.Sprintf("host=%v ", primaryHostname) +
		"user=postgres " +
		"port=5432 " +
		fmt.Sprintf("sslkey=%v ", postgres.PostgresKeyLocation) +
		fmt.Sprintf("sslcert=%v ", postgres.PostgresCertificateLocation) +
		fmt.Sprintf("sslrootcert=%v ", postgres.CACertificateLocation) +
		"sslmode=require"
	return primaryConnInfo
}
