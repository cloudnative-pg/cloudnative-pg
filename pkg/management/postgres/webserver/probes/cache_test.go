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

var _ = Describe("clusterCache", func() {
	var (
		ctx      context.Context
		cli      client.Client
		instance *postgres.Instance
		cache    *clusterCache
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

		cache = newClusterCache(cli, client.ObjectKey{
			Namespace: instance.GetNamespaceName(),
			Name:      instance.GetClusterName(),
		})
	})

	Context("tryGetLatestClusterWithTimeout", func() {
		It("should successfully refresh the cluster definition", func() {
			cluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeTrue())
			Expect(cluster).ToNot(BeNil())
			Expect(cluster.Name).To(Equal("test-cluster"))
			Expect(cluster.Spec.Instances).To(Equal(3))
		})

		It("should return false when the cluster is not found", func() {
			cache = newClusterCache(cli, client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "non-existent-cluster",
			})

			cluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeFalse())
			Expect(cluster).To(BeNil())
		})

		It("should handle context cancellation", func() {
			// Create a context that is already cancelled
			cancelledCtx, cancel := context.WithCancel(ctx)
			cancel()

			// With a cancelled context, the operation should fail
			// Note: With fake client, this might still succeed if it's fast enough,
			// but in real scenarios with actual API server delays, this would fail
			_, _ = cache.tryGetLatestClusterWithTimeout(cancelledCtx)

			// We don't assert the result here because the fake client is too fast
			// This test mainly verifies the code doesn't panic with a cancelled context
		})

		It("should cache the cluster definition across multiple calls", func() {
			// First refresh
			firstCluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeTrue())
			Expect(firstCluster).ToNot(BeNil())
			Expect(firstCluster.Spec.Instances).To(Equal(3))

			// Update the cluster
			cluster.Spec.Instances = 5
			err := cli.Update(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Second refresh should get the updated cluster
			secondCluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeTrue())
			Expect(secondCluster).ToNot(BeNil())
			Expect(secondCluster.Spec.Instances).To(Equal(5))
		})

		It("should maintain the cached cluster when refresh fails", func() {
			// First successful refresh
			cachedCluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeTrue())
			Expect(cachedCluster).ToNot(BeNil())

			// Change cache to point to non-existent cluster
			cache.key = client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "non-existent-cluster",
			}

			// Second refresh should fail but cache should remain
			stillCachedCluster, success := cache.tryGetLatestClusterWithTimeout(ctx)
			Expect(success).To(BeFalse())
			// Cache should still have the old cluster
			Expect(stillCachedCluster).To(Equal(cachedCluster))
		})
	})
})
