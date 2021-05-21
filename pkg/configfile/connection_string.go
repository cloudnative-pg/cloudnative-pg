/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// CreateConnectionString create a PostgreSQL connection string given the
// passed parameters, escaping them as necessary
func CreateConnectionString(parameters map[string]string) string {
	wr := strings.Builder{}

	for key, value := range parameters {
		wr.WriteString(escapeConnectionStringParameter(key, value))
		wr.WriteString(" ")
	}

	return strings.TrimRight(wr.String(), " ")
}

func escapeConnectionStringParameter(key, value string) string {
	return fmt.Sprintf("%v=%v", key, pq.QuoteLiteral(value))
}
