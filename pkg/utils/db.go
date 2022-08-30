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

package utils

import (
	"database/sql"

	"github.com/jackc/pgx/v4"
	// this is needed to correctly open the sql connection with the pgx driver. Do not remove this import.
	"github.com/jackc/pgx/v4/stdlib"
)

// NewSimpleDBConnection creates a postgres connection with the simple protocol
func NewSimpleDBConnection(connectionString string) (*sql.DB, error) {
	conf, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, err
	}
	conf.PreferSimpleProtocol = true
	// this is required, do not remove.
	conf.RuntimeParams["client_encoding"] = "UTF8"

	return sql.Open("pgx", stdlib.RegisterConnConfig(conf))
}
