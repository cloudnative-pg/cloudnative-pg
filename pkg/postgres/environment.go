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
	"path/filepath"
	"regexp"
	"strings"
)

// envPlaceholderRegexp matches one or more dollar signs followed by {name}.
// The number of dollar signs determines whether it is a placeholder or an
// escape sequence: an odd count means the last $ starts a real placeholder,
// while an even count means all dollars are escape pairs and the braces are
// literal.
var envPlaceholderRegexp = regexp.MustCompile(`(\$+)\{([^}]+)\}`)

// knownPlaceholders is the set of supported placeholders in extension env var values.
var knownPlaceholders = map[string]bool{
	"image_root": true,
}

// ExpandEnvPlaceholders expands supported placeholders in value for the given
// extension and unescapes $$ pairs preceding braces.
func ExpandEnvPlaceholders(value string, extensionName string) string {
	return envPlaceholderRegexp.ReplaceAllStringFunc(value, func(match string) string {
		dollars := 0
		for dollars < len(match) && match[dollars] == '$' {
			dollars++
		}
		prefix := strings.Repeat("$", dollars/2)
		if dollars%2 == 0 {
			// Even: all dollars are escape pairs, braces are literal.
			return prefix + match[dollars:]
		}
		// Odd: last $ starts a real placeholder.
		// NOTE: cases here must match the entries in knownPlaceholders above.
		name := match[dollars+1 : len(match)-1]
		switch name {
		case "image_root":
			return prefix + filepath.Join(ExtensionsBaseDirectory, extensionName)
		default:
			// Unknown placeholder: leave as-is (runtime warns separately).
			return match
		}
	})
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
	var unknown []string
	for _, match := range envPlaceholderRegexp.FindAllStringSubmatch(value, -1) {
		// Only odd dollar counts are real placeholders.
		if len(match[1])%2 == 1 && !knownPlaceholders[match[2]] {
			unknown = append(unknown, "${"+match[2]+"}")
		}
	}
	return unknown
}
