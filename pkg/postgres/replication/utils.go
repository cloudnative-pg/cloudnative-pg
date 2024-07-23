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

package replication

import (
	"fmt"
	"strings"
)

// escapePostgresLiteral escapes a value to make its representation
// similar to the literals in PostgreSQL
func escapePostgresConfLiteral(value string) string {
	return fmt.Sprintf("\"%v\"", strings.ReplaceAll(value, "\"", "\"\""))
}
