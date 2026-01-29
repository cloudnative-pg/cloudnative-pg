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

package v1

import (
	"fmt"
)

type managedResourceComparer interface {
	GetName() string
	GetManagedObjectName() string
	GetClusterRef() ClusterObjectReference
	GetClusterNamespace() string
	HasReconciliations() bool
}

func ensureManagedResourceExclusivity[T managedResourceComparer](t1 T, list []T) error {
	for _, t2 := range list {
		if t1.GetName() == t2.GetName() {
			continue
		}

		// Check if both resources reference the same cluster (name and namespace)
		if t1.GetClusterRef().Name != t2.GetClusterRef().Name {
			continue
		}

		if t1.GetClusterNamespace() != t2.GetClusterNamespace() {
			continue
		}

		if !t2.HasReconciliations() {
			continue
		}

		if t1.GetManagedObjectName() == t2.GetManagedObjectName() {
			return fmt.Errorf(
				"%q is already managed by object %q",
				t1.GetManagedObjectName(), t2.GetName(),
			)
		}
	}

	return nil
}

// toSliceWithPointers converts a slice of items to a slice of pointers to the items
func toSliceWithPointers[T any](items []T) []*T {
	result := make([]*T, len(items))
	for i := range items {
		result[i] = &items[i]
	}
	return result
}
