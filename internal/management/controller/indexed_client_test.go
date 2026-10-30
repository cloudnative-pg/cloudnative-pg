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

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cloudnative-pg/cloudnative-pg/internal/clusterindex"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
)

// newIndexedFakeClientBuilder returns a fake client builder registering the
// same cluster index the instance manager creates, which the reconcilers query
// to find the resources of a cluster in any namespace
func newIndexedFakeClientBuilder() *fake.ClientBuilder {
	builder := fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme())
	for _, obj := range clusterindex.IndexedObjects() {
		builder = builder.WithIndex(obj, clusterindex.Key, clusterindex.IndexByCluster)
	}
	return builder
}
