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

package postgres

import "strings"

const (
	operatorReservedRolesPrefix   = "cnpg_"
	postgresqlReservedRolesPrefix = "pg_"
)

// IsRoleReserved checks if a role is reserved for PostgreSQL
// or the operator
func IsRoleReserved(name string) bool {
	// Check for roles reserved for the operator
	operatorReservedRoles := map[string]interface{}{
		"streaming_replica": nil,
		"postgres":          nil,
	}
	if _, isReserved := operatorReservedRoles[name]; isReserved {
		return isReserved
	}

	if strings.HasPrefix(name, operatorReservedRolesPrefix) {
		return true
	}

	// Check for the roles reserved for PostgreSQL
	if strings.HasPrefix(name, postgresqlReservedRolesPrefix) {
		return true
	}

	return false
}
