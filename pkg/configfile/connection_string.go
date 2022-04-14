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
