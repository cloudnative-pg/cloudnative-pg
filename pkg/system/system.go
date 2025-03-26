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

// Package system provides an interface with the operating system
package system

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system/compatibility"
)

const (
	// DefaultCoredumpFilter it's the default value for the /proc/self/coredump_filter file
	DefaultCoredumpFilter = "0x31"
)

// SetCoredumpFilter write the content of filter to coredump_filter of the current pid
func SetCoredumpFilter(coredumpFilter string) error {
	return compatibility.SetCoredumpFilter(coredumpFilter)
}
