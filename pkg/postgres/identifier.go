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

// Package postgres contains the function covering the PostgreSQL
// integrations and the relative data types
package postgres

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// postgresIdentifierMaxLen is the maximum length PostgreSQL allows for identifiers
	postgresIdentifierMaxLen int = 63

	// SystemTablespacesPrefix is the prefix denoting tablespaces managed by the Postgres system
	// see https://www.postgresql.org/docs/current/sql-createtablespace.html
	SystemTablespacesPrefix = "pg_"
)

// regex to verify a Postgres-compliant identifier
// see https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS
var postgresIdentifierRegex = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_$]*$")

// IsTablespaceNameValid check if tablespace name is valid or not
func IsTablespaceNameValid(name string) (bool, error) {
	if strings.HasPrefix(name, SystemTablespacesPrefix) {
		return false, fmt.Errorf("tablespace names beginning 'pg_' are reserved for Postgres")
	}

	if !postgresIdentifierRegex.MatchString(name) {
		return false, fmt.Errorf("tablespace names must be valid Postgres identifiers: " +
			"alphanumeric characters, '_', '$', and must start with a letter or an underscore")
	}

	if len(name) > postgresIdentifierMaxLen {
		return false, fmt.Errorf("the maximum length of an identifier is 63 characters")
	}

	return true, nil
}
