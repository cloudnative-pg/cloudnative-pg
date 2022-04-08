/*
Copyright 2019-2022 The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"fmt"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// buildPrimaryConnInfo builds the connection string to connect to primaryHostname
func buildPrimaryConnInfo(primaryHostname, applicationName string) string {
	// We should have been using configfile.CreateConnectionString
	// but doing that we would cause an unnecessary restart of
	// existing PostgreSQL 12 clusters.
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
