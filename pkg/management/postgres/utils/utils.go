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

package utils

// This code is inspired on [postgres_exporter](https://github.com/prometheus-community/postgres_exporter)

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// DBToFloat64 convert a dynamic value to float64s for Prometheus consumption. Null types are mapped to NaN. string
// and []byte types are mapped as NaN and !ok
func DBToFloat64(t interface{}) (float64, bool) {
	switch v := t.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case time.Time:
		return float64(v.Unix()), true
	case []byte:
		// Try and convert to string and then parse to a float64
		strV := string(v)
		result, err := strconv.ParseFloat(strV, 64)
		if err != nil {
			return math.NaN(), false
		}
		return result, true
	case string:
		result, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return math.NaN(), false
		}
		return result, true
	case bool:
		if v {
			return 1.0, true
		}
		return 0.0, true
	case nil:
		return math.NaN(), true
	default:
		return math.NaN(), false
	}
}

// DBToUint64 convert a dynamic type to uint64 for Prometheus consumption. Null types are mapped to 0. string and []byte
// types are mapped as 0 and !ok
func DBToUint64(t interface{}) (uint64, bool) {
	switch v := t.(type) {
	case uint64:
		return v, true
	case int64:
		return uint64(v), true //nolint:gosec
	case float64:
		return uint64(v), true
	case time.Time:
		return uint64(v.Unix()), true //nolint:gosec
	case []byte:
		// Try and convert to string and then parse to a uint64
		strV := string(v)
		result, err := strconv.ParseUint(strV, 10, 64)
		if err != nil {
			return 0, false
		}
		return result, true
	case string:
		result, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return result, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	default:
		return 0, false
	}
}

// DBToString convert a dynamic type to string for Prometheus labels. Null types are mapped to empty strings.
func DBToString(t interface{}) (string, bool) {
	switch v := t.(type) {
	case int64:
		return fmt.Sprintf("%v", v), true
	case float64:
		return fmt.Sprintf("%v", v), true
	case time.Time:
		return fmt.Sprintf("%v", v.Unix()), true
	case nil:
		return "", true
	case []byte:
		// Try and convert to string
		return string(v), true
	case string:
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// GetAllAccessibleDatabases returns the list of all the accessible databases using the superuser
func GetAllAccessibleDatabases(tx *sql.Tx, whereClause string) (databases []string, errors []error) {
	rows, err := tx.Query(strings.Join(
		[]string{"SELECT datname FROM pg_catalog.pg_database", whereClause},
		" WHERE "),
	)
	if err != nil {
		return nil, []error{fmt.Errorf("could not get databases: %w", err)}
	}
	defer func() {
		err = rows.Close()
		if err != nil {
			log.Error(err, "while closing rows")
		}
	}()
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			errors = append(errors, fmt.Errorf("could not parse a row: %w", err))
		} else {
			databases = append(databases, database)
		}
	}
	if err = rows.Err(); err != nil {
		errors = append(errors, err)
	}
	if len(errors) > 0 {
		return databases, errors
	}
	return databases, nil
}
