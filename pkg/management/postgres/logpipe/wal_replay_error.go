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

package logpipe

import (
	"strings"
)

// walReplayErrorPatterns contains the PostgreSQL log message substrings that
// indicate an unrecoverable WAL replay failure. When any of these patterns is
// found in a log line the instance should be marked unhealthy so that
// Kubernetes stops routing traffic to it and the operator can exclude it from
// failover candidate selection.
//
// The patterns are intentionally short substrings rather than full regular
// expressions so that they match across all PostgreSQL versions regardless of
// the surrounding LSN / offset values.
var walReplayErrorPatterns = []string{
	// Emitted when the WAL record header contains a back-link that does not
	// point to the expected previous record position.
	"record with incorrect prev-link",

	// Emitted when the WAL page header flags indicate that the current record
	// is a continuation of a split record, but the record reader does not
	// expect a continuation here.
	"contrecord is requested by",
}

// IsWALReplayErrorLine returns true when the provided log line contains one of
// the known fatal WAL replay error patterns.  The check is intentionally
// case-insensitive so that it works regardless of how PostgreSQL capitalises
// the message in future versions.
func IsWALReplayErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, pattern := range walReplayErrorPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
