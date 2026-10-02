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

// DataChecksumsInitdbFlag returns the initdb flag needed to make the
// requested data checksums setting explicit for the given PostgreSQL major
// version, or the empty string when the request already matches that
// version's initdb default.
//
// Before PostgreSQL 18, initdb disables data checksums by default, so
// "--data-checksums" is only needed when enabling checksums. Starting
// from PostgreSQL 18, initdb enables data checksums by default, so
// "--no-data-checksums" is only needed when disabling checksums.
func DataChecksumsInitdbFlag(majorVersion int, enabled bool) string {
	if majorVersion < 18 {
		if enabled {
			return "--data-checksums"
		}
		return ""
	}

	if !enabled {
		return "--no-data-checksums"
	}
	return ""
}
