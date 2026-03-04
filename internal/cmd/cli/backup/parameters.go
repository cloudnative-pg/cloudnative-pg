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

package backup

import (
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
)

// pluginParameters is a set of parameters to be passed
// to the plugin when taking a backup
type pluginParameters map[string]string

// String implements the pflag.Value interface
func (e pluginParameters) String() string {
	return strings.Join(stringset.FromKeys(e).ToList(), ",")
}

// Type implements the pflag.Value interface
func (e pluginParameters) Type() string {
	return "map[string]string"
}

// Set implements the pflag.Value interface
func (e *pluginParameters) Set(val string) error {
	entries := strings.Split(val, ",")
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}

		before, after, _ := strings.Cut(entry, "=")
		result[before] = after
	}
	*e = result
	return nil
}
