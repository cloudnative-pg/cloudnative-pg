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

package tablespaces

import (
	"github.com/cloudnative-pg/machinery/pkg/fileutils"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// tablespaceStorageManager represents the required behavior in terms of storage
// for the tablespace reconciler
type tablespaceStorageManager interface {
	getStorageLocation(tbsName string) string
	storageExists(tbsName string) (bool, error)
}

type instanceTablespaceStorageManager struct{}

func (ism instanceTablespaceStorageManager) getStorageLocation(tbsName string) string {
	return specs.LocationForTablespace(tbsName)
}

func (ism instanceTablespaceStorageManager) storageExists(tbsName string) (bool, error) {
	return fileutils.FileExists(ism.getStorageLocation(tbsName))
}
