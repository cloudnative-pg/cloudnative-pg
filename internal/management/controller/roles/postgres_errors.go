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

package roles

import (
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// getRoleError matches an error to one of the expectable RoleError's
// If it does not match, it will simply pass the original error along
//
// For PostgreSQL codes see https://www.postgresql.org/docs/current/errcodes-appendix.html
func getRoleError(err error, roleName string, action roleAction) (bool, error) {
	errPGX, ok := err.(*pgconn.PgError)
	if !ok {
		return false, err
	}
	switch pq.ErrorCode(errPGX.Code).Name() {
	case "dependent_objects_still_exist":
		// code 2BP01
		fallthrough
	case "undefined_object":
		// code 42704
		return true, RoleError{
			Action:   string(action),
			RoleName: roleName,
			Cause:    errPGX.Detail,
		}
	default:
		return false, err
	}
}
