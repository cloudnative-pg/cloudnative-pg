/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package custom

// This code is heavily based on [pg_exporter](https://github.com/prometheus-community/postgres_exporter)
// since we are reusing the custom query infrastructure that that project already have.

import (
	"fmt"
	"math"
	"strconv"
	"time"
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
		return uint64(v), true
	case float64:
		return uint64(v), true
	case time.Time:
		return uint64(v.Unix()), true
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
