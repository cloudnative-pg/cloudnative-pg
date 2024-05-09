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

import (
	"slices"
	"strings"
)

// IsTrue returns true if and only iff the string represents a positive/true
// value for Postgres configuration
// See: https://www.postgresql.org/docs/current/config-setting.html
// Boolean: Values can be written as on, off, true, false, yes, no, 1, 0 (all case-insensitive)
func IsTrue(in string) bool {
	trueValues := []string{"1", "on", "yes", "true"}
	sanitized := strings.TrimSpace(in)
	sanitized = strings.ToLower(sanitized)
	return slices.Contains(trueValues, sanitized)
}

// IsFalse returns true if and only iff the string represents a negative/false
// value for Postgres configuration
// See: https://www.postgresql.org/docs/current/config-setting.html
// Boolean: Values can be written as on, off, true, false, yes, no, 1, 0 (all case-insensitive)
func IsFalse(in string) bool {
	falseValues := []string{"0", "no", "off", "false"}
	sanitized := strings.TrimSpace(in)
	sanitized = strings.ToLower(sanitized)
	return slices.Contains(falseValues, sanitized)
}
