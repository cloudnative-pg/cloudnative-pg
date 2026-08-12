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

package run

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getDatabaseCacheConfig", func() {
	const (
		namespace   = "postgres"
		clusterName = "cluster-example"
	)

	newCluster := func(crossNamespace bool) *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				Instances:                     1,
				EnableCrossNamespaceDatabases: crossNamespace,
			},
		}
	}

	It("restricts the watch to the cluster namespace by default", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(newCluster(false)).
			Build()

		config, err := getDatabaseCacheConfig(ctx, cli, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(config.Namespaces).To(HaveKey(namespace))
		Expect(config.Namespaces).To(HaveLen(1))
	})

	It("watches every namespace when cross-namespace databases are enabled", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(newCluster(true)).
			Build()

		config, err := getDatabaseCacheConfig(ctx, cli, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(Equal(cache.ByObject{}))
	})

	It("fails when the cluster cannot be read", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()

		_, err := getDatabaseCacheConfig(ctx, cli, namespace, clusterName)
		Expect(err).To(HaveOccurred())
	})
})
