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

import "strings"

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
