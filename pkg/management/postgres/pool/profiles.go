/*
Copyright The CloudNativePG Contributors

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

package pool

import "github.com/jackc/pgx/v5"

var (
	// ConnectionProfilePostgresql is the connection profile to be used for PostgreSQL
	ConnectionProfilePostgresql ConnectionProfile = connectionProfilePostgresql{}

	// ConnectionProfilePostgresqlPhysicalReplication is the connection profile to be used for PostgreSQL
	// using the physical replication protocol
	ConnectionProfilePostgresqlPhysicalReplication ConnectionProfile = connectionProfilePostgresqlPhysicalReplication{}

	// ConnectionProfilePgbouncer is the connection profile to be used for Pgbouncer
	ConnectionProfilePgbouncer ConnectionProfile = connectionProfilePgbouncer{}
)

type connectionProfilePostgresql struct{}

func (connectionProfilePostgresql) Enrich(config *pgx.ConnConfig) {
	fillDefaultParameters(config)
}

type connectionProfilePostgresqlPhysicalReplication struct{}

func (connectionProfilePostgresqlPhysicalReplication) Enrich(config *pgx.ConnConfig) {
	fillDefaultParameters(config)

	// The simple query protocol is needed since we're going to use
	// this function to connect to the PgBouncer administrative
	// interface, which doesn't support the extended one.
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// To initiate streaming replication, the frontend sends the replication parameter
	// in the startup message. A Boolean value of true (or on, yes, 1) tells the backend
	// to go into physical replication walsender mode, wherein a small set of replication
	// commands, shown below, can be issued instead of SQL statements.
	// https://www.postgresql.org/docs/current/protocol-replication.html
	config.RuntimeParams["replication"] = "1"
}

type connectionProfilePgbouncer struct{}

func (connectionProfilePgbouncer) Enrich(config *pgx.ConnConfig) {
	fillDefaultParameters(config)

	// The simple query protocol is needed since we're going to use
	// this function to connect to the PgBouncer administrative
	// interface, which doesn't support the extended one.
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
}

func fillDefaultParameters(config *pgx.ConnConfig) {
	// This is required by pgx when using the simple protocol during
	// the sanitization of the strings. Do not remove.
	config.RuntimeParams["client_encoding"] = "UTF8"

	// Set the default datestyle in the connection helps to keep
	// a standard date format for the operator to manage the dates
	// when it's needed
	config.RuntimeParams["datestyle"] = "ISO"
}
