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

package tablespaces

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// TablespaceError is an EXPECTABLE error when performing tablespace-related actions on the
// database. For example, we might try to create a tablespace with an owner that does not
// exist
//
// TablespaceError is NOT meant to represent unexpected errors such as a panic or a
// connection interruption
type TablespaceError struct {
	TablespaceName string
	Cause          string
	Action         string
}

// Error returns a description for the error,
// … and lets TablespaceError comply with the `error` interface
func (re TablespaceError) Error() string {
	return fmt.Sprintf("could not perform %s on tablespace %s: %s",
		re.Action, re.TablespaceName, re.Cause)
}

// getTablespaceError matches an error to one of the expectable causes and
// if there is a match, generates a TablespaceError with the detail
// If it does not match, it will simply pass the original error along
//
// For PostgreSQL codes see https://www.postgresql.org/docs/current/errcodes-appendix.html
func getTablespaceError(err error, tbsName string, action TablespaceAction) (bool, error) {
	errPGX, ok := err.(*pgconn.PgError)
	if !ok {
		// before giving up, let's see if there is an un-wrapped error
		coreErr := errors.Unwrap(err)
		errPGX, ok = coreErr.(*pgconn.PgError)
		if !ok {
			return false, fmt.Errorf("while trying to %s: %w", action, err)
		}
	}

	knownCauses := map[string]string{
		"42704": errPGX.Message, // 42704 -> undefined_object
		"0LP01": errPGX.Message, // 0LP01 -> invalid_grant_operation
	}

	if cause, known := knownCauses[errPGX.Code]; known {
		return true, TablespaceError{
			Action:         string(action),
			TablespaceName: tbsName,
			Cause:          cause,
		}
	}
	return false, fmt.Errorf("while trying to %s: %w", action, err)
}
