/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package configfile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// CreateConnectionString create a PostgreSQL connection string given the
// passed parameters, escaping them as necessary
func CreateConnectionString(parameters map[string]string) string {
	wr := strings.Builder{}

	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, key := range keys {
		wr.WriteString(escapeConnectionStringParameter(key, parameters[key]))
		wr.WriteString(" ")
	}

	return strings.TrimRight(wr.String(), " ")
}

func escapeConnectionStringParameter(key, value string) string {
	return fmt.Sprintf("%v=%v", key, pq.QuoteLiteral(value))
}
