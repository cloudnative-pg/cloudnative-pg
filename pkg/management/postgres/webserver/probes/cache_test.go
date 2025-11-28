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

package probes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ClusterCache", func() {
	var (
		ctx      context.Context
		cli      client.Client
		instance *postgres.Instance
		cache    *ClusterCache
		cluster  *apiv1.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
			},
		}

		cli = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		instance = postgres.NewInstance().
			WithNamespace("test-namespace").
			WithClusterName("test-cluster")

		cache = NewClusterCache(cli, client.ObjectKey{
			Namespace: instance.GetNamespaceName(),
			Name:      instance.GetClusterName(),
		})
	})

	Context("tryGetLatestClusterWithTimeout", func() {
		It("should successfully refresh the cluster definition", func() {
			var cluster apiv1.Cluster
			err := cache.tryGetLatestClusterWithTimeout(ctx, &cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Name).To(Equal("test-cluster"))
			Expect(cluster.Spec.Instances).To(Equal(3))
		})

		It("should return an error when the cluster is not found", func() {
			cache = NewClusterCache(cli, client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "non-existent-cluster",
			})

			var cluster apiv1.Cluster
			err := cache.tryGetLatestClusterWithTimeout(ctx, &cluster)
			Expect(err).To(HaveOccurred())
			Expect(cluster.Name).To(BeEmpty())
		})

		It("should cache the cluster definition across multiple calls", func() {
			// First refresh
			var firstCluster apiv1.Cluster
			err := cache.tryGetLatestClusterWithTimeout(ctx, &firstCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(firstCluster.Spec.Instances).To(Equal(3))

			// Update the cluster
			cluster.Spec.Instances = 5
			err = cli.Update(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Second refresh should get the updated cluster
			var secondCluster apiv1.Cluster
			err = cache.tryGetLatestClusterWithTimeout(ctx, &secondCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(secondCluster.Spec.Instances).To(Equal(5))
		})

		It("should maintain the cached cluster when refresh fails", func() {
			// First successful refresh
			var cachedCluster apiv1.Cluster
			err := cache.tryGetLatestClusterWithTimeout(ctx, &cachedCluster)
			Expect(err).ToNot(HaveOccurred())

			// Change cache to point to non-existent cluster
			cache.key = client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "non-existent-cluster",
			}

			// Second refresh should fail but cache should remain
			var stillCachedCluster apiv1.Cluster
			err = cache.tryGetLatestClusterWithTimeout(ctx, &stillCachedCluster)
			Expect(err).To(HaveOccurred())
			// Cache should still have the old cluster
			Expect(stillCachedCluster).To(Equal(cachedCluster))
		})
	})
})
