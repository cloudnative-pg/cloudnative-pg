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

package postgres

import (
	"regexp"
	"strings"
)

// placeholderRegexp matches ${...} patterns in env var values.
var placeholderRegexp = regexp.MustCompile(`\$\{([^}]+)\}`)

// escapedPlaceholderRegexp matches $${...} escape sequences.
var escapedPlaceholderRegexp = regexp.MustCompile(`\$\$\{[^}]+\}`)

// knownPlaceholders is the set of supported placeholders in extension env var values.
// Keep in sync with extensionEnvPlaceholders in pkg/management/postgres/instance.go.
var knownPlaceholders = map[string]bool{
	"image_root": true,
}

// IsReservedEnvironmentVariable detects if a certain environment variable
// is reserved for the usage of the operator.
func IsReservedEnvironmentVariable(name string) bool {
	name = strings.ToUpper(name)

	switch {
	case strings.HasPrefix(name, "CNPG_"):
		return true

	case strings.HasPrefix(name, "PG"):
		return true

	case name == "POD_NAME":
		return true

	case name == "NAMESPACE":
		return true

	case name == "CLUSTER_NAME":
		return true
	}

	return false
}

// FindUnknownPlaceholders returns the list of unrecognized ${...} placeholders
// found in the given value. Escaped placeholders ($${...}) are ignored.
func FindUnknownPlaceholders(value string) []string {
	// Strip escaped placeholders before scanning for unescaped ones.
	stripped := escapedPlaceholderRegexp.ReplaceAllString(value, "")
	matches := placeholderRegexp.FindAllStringSubmatch(stripped, -1)
	var unknown []string
	for _, match := range matches {
		if !knownPlaceholders[match[1]] {
			unknown = append(unknown, "${"+match[1]+"}")
		}
	}
	return unknown
}
