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

import (
	"database/sql"

	"github.com/jackc/pgx/v5"
	// this is needed to correctly open the sql connection with the pgx driver. Do not remove this import.
	"github.com/jackc/pgx/v5/stdlib"
)

// ConnectionProfile represent a predefined set of connection configuration
type ConnectionProfile interface {
	// Enrich applies the configuration of the profile to a connection configuration
	Enrich(config *pgx.ConnConfig)
}

var (
	// ConnectionProfilePostgresql is the connection profile to be used for PostgreSQL
	ConnectionProfilePostgresql ConnectionProfile = connectionProfilePostgresql{}

	// ConnectionProfilePgbouncer is the connection profile to be used for Pgbouncer
	ConnectionProfilePgbouncer ConnectionProfile = connectionProfilePgbouncer{}
)

type connectionProfilePostgresql struct{}

func (connectionProfilePostgresql) Enrich(config *pgx.ConnConfig) {
	// This is required by pgx when using the simple protocol during
	// the sanitization of the strings. Do not remove.
	config.RuntimeParams["client_encoding"] = "UTF8"

	// Set the default datestyle in the connection helps to keep
	// a standard date format for the operator to manage the dates
	// when it's needed
	config.RuntimeParams["datestyle"] = "ISO"
}

type connectionProfilePgbouncer struct{}

func (connectionProfilePgbouncer) Enrich(config *pgx.ConnConfig) {
	// The simple query protocol is needed since we're going to use
	// this function to connect to the PgBouncer administrative
	// interface, which doesn't support the extended one.
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// This is required by pgx when using the simple protocol during
	// the sanitization of the strings. Do not remove.
	config.RuntimeParams["client_encoding"] = "UTF8"

	// Set the default datestyle in the connection helps to keep
	// a standard date format for the operator to manage the dates
	// when it's needed
	config.RuntimeParams["datestyle"] = "ISO"
}

// NewDBConnection creates a postgres connection with the simple protocol
func NewDBConnection(connectionString string, profile ConnectionProfile) (*sql.DB, error) {
	conf, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, err
	}
	profile.Enrich(conf)

	return sql.Open("pgx", stdlib.RegisterConnConfig(conf))
}
