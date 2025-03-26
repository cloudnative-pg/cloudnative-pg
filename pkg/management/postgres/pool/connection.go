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

// NewDBConnection creates a postgres connection with the simple protocol
func NewDBConnection(connectionString string, profile ConnectionProfile) (*sql.DB, error) {
	conf, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, err
	}
	profile.Enrich(conf)

	return sql.Open("pgx", stdlib.RegisterConnConfig(conf))
}
