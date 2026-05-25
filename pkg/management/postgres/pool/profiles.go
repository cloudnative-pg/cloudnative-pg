/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package pool

import "github.com/jackc/pgx/v5"

var (
	// ConnectionProfilePostgresql is the connection profile to be used for PostgreSQL
	ConnectionProfilePostgresql connectionProfilePostgresql

	// ConnectionProfilePostgresqlPhysicalReplication is the connection profile to be used for PostgreSQL
	// using the physical replication protocol
	ConnectionProfilePostgresqlPhysicalReplication connectionProfilePostgresqlPhysicalReplication

	// ConnectionProfilePgbouncer is the connection profile to be used for Pgbouncer
	ConnectionProfilePgbouncer connectionProfilePgbouncer
)

type profile struct{}

type connectionProfilePostgresql profile

func (connectionProfilePostgresql) Enrich(config *pgx.ConnConfig) {
	fillDefaultParameters(config)

	// We don't want to be stuck on queries if synchronous replicas
	// are still not alive and kicking. The next reconciliation loop
	// can keep track of them if needed.
	config.RuntimeParams["synchronous_commit"] = "local"
}

type connectionProfilePostgresqlPhysicalReplication profile

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

type connectionProfilePgbouncer profile

func (connectionProfilePgbouncer) Enrich(config *pgx.ConnConfig) {
	fillDefaultParameters(config)

	// The simple query protocol is needed since we're going to use
	// this function to connect to the PgBouncer administrative
	// interface, which doesn't support the extended one.
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// The PgBouncer admin console does not execute catalog SQL and
	// only accepts the startup parameters listed in
	// ignore_startup_parameters (extra_float_digits,options by default).
	// The pinned search_path is therefore unnecessary here and would
	// cause the admin connection to be rejected.
	delete(config.RuntimeParams, "search_path")
}

func fillDefaultParameters(config *pgx.ConnConfig) {
	// This is required by pgx when using the simple protocol during
	// the sanitization of the strings. Do not remove.
	config.RuntimeParams["client_encoding"] = "UTF8"

	// Set the default datestyle in the connection helps to keep
	// a standard date format for the operator to manage the dates
	// when it's needed
	config.RuntimeParams["datestyle"] = "ISO"

	// Pin search_path so a tenant-controlled ALTER DATABASE / ALTER ROLE
	// setting cannot influence operator-issued queries. pg_catalog is
	// first so any built-in overload defeats a planted shadow in public.
	// public stays in the path so CREATE EXTENSION (in initdb post-init
	// and the Database controller) can resolve required-extension types
	// — PostgreSQL's CREATE EXTENSION uses the connection-level value
	// when computing the install-time search_path, not the session-level
	// SET.
	config.RuntimeParams["search_path"] = "pg_catalog, public"
}
