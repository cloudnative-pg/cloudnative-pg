//go:build linux

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

// Package compatibility provides a layer to cross-compile with other OS than Linux
package compatibility

import (
	"os"
)

// SetCoredumpFilter set the value of /proc/self/coredump_filter
func SetCoredumpFilter(coredumpFilter string) error {
	coredumpFilterFile := "/proc/self/coredump_filter"
	return os.WriteFile(coredumpFilterFile, []byte(coredumpFilter), 0o600)
}
