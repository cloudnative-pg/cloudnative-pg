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
	"fmt"
	"slices"
	"strings"
)

// ParsePostgresBoolean returns the boolean value parsed from a string as a postgres boolean.
// It returns an error if the input string is not a valid postgres boolean
// See: https://www.postgresql.org/docs/current/config-setting.html
// Boolean: Values can be written as on, off, true, false, yes, no, 1, 0 (all case-insensitive)
// or any unambiguous prefix of one of these.
func ParsePostgresBoolean(in string) (bool, error) {
	trueValues := []string{"1", "on", "yes", "true", "y", "t", "ye", "tr", "tru"}
	falseValues := []string{"0", "no", "off", "false", "n", "f", "of", "fa", "fal", "fals"}

	sanitized := strings.TrimSpace(in)
	sanitized = strings.ToLower(sanitized)

	if slices.Contains(falseValues, sanitized) {
		return false, nil
	}
	if slices.Contains(trueValues, sanitized) {
		return true, nil
	}
	return false, fmt.Errorf("configuration value is not a postgres boolean: %s", in)
}
