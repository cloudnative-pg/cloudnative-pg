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

package syncsequences

import (
	"fmt"

	"github.com/lib/pq"
)

// CreateSyncScript creates a SQL script to synchronize the sequences
// in the destination database with the status of the source database
func CreateSyncScript(source, destination SequenceMap, offset int) string {
	script := ""

	for name := range destination {
		targetValue, ok := source[name]
		if !ok {
			// This sequence is not available in the source database,
			// there's no need to update it
			continue
		}

		sqlTargetValue := "NULL"
		if targetValue != nil {
			sqlTargetValue = fmt.Sprintf("%d", *targetValue)
			if offset != 0 {
				sqlTargetValue = fmt.Sprintf("%s + %d", sqlTargetValue, offset)
			}
		}

		script += fmt.Sprintf(
			"SELECT pg_catalog.setval(%s, %v);\n",
			pq.QuoteLiteral(name),
			sqlTargetValue)
	}

	return script
}
