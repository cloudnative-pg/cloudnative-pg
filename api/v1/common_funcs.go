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

// namespaceOrDefault returns the namespace of the referenced Cluster, which
// is the passed namespace of the referring object when it is not specified
func (ref ClusterObjectReference) namespaceOrDefault(namespace string) string {
	if ref.Namespace != "" {
		return ref.Namespace
	}
	return namespace
}

// isCrossNamespace tells whether the referenced Cluster lives in a namespace
// other than the passed one of the referring object
func (ref ClusterObjectReference) isCrossNamespace(namespace string) bool {
	return ref.namespaceOrDefault(namespace) != namespace
}
