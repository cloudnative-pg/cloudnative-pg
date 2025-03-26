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

package pgpass

import (
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
)

// Data represents the contents of a pgpass file
type Data struct {
	info []ConnectionInfo
}

// From creates a new pgpass file content with the
// specified lines
func From(lines ...ConnectionInfo) *Data {
	return &Data{
		info: lines,
	}
}

// Empty creates an empty pgpass file content
func Empty() *Data {
	return &Data{}
}

// WithLine appends a line to an existing pgpass file content
func (data *Data) WithLine(line ConnectionInfo) *Data {
	data.info = append(data.info, line)
	return data
}

// Write writes the content of this data file into the chosen file
// name, truncating if existing
func (data *Data) Write(fileName string) error {
	lines := make([]string, len(data.info))
	for i, line := range data.info {
		lines[i] = line.BuildLine()
	}

	_, err := fileutils.WriteFileAtomic(fileName, []byte(strings.Join(lines, "\n")), 0o600)
	return err
}
