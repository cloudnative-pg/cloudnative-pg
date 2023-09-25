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

package utils

import "strings"

// ParsePgControldataOutput parses a pg_controldata output into a map of key-value pairs
func ParsePgControldataOutput(data string) map[string]string {
	pairs := make(map[string]string)
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		frags := strings.Split(line, ":")
		if len(frags) != 2 {
			continue
		}
		pairs[strings.TrimSpace(frags[0])] = strings.TrimSpace(frags[1])
	}
	return pairs
}
