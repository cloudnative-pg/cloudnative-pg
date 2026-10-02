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

// Package logs provides assertions over the JSON-encoded postgres logs
// captured from a pod. Callers that also import tests/utils/logs should
// alias one of the two to avoid the package name collision.
package logs

import (
	logsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/logs"
)

// HasQueryRecord reports whether any of the JSON log entries contains
// the expected query, message and logger triple. It is a predicate, not
// an Assert*-style helper, so it returns a bool for callers to feed into
// Eventually(...).Should(BeTrue()).
func HasQueryRecord(logEntries []map[string]interface{}, errorTestQuery, message, logger string) bool {
	for _, logEntry := range logEntries {
		if logsutils.IsWellFormedLogForLogger(logEntry, logger) &&
			logsutils.CheckRecordForQuery(logEntry, errorTestQuery, "postgres", "app", message) {
			return true
		}
	}
	return false
}
