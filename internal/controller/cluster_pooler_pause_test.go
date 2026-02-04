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
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// poolerClusterKeyIndexAdapter filters pooler list based on cluster name index
func poolerClusterKeyIndexAdapter(list client.ObjectList, opts ...client.ListOption) client.ObjectList {
	var clusterName string
	for _, opt := range opts {
		res, ok := opt.(client.MatchingFields)
		if !ok {
			continue
		}
		if name, exists := res[poolerClusterKey]; exists {
			clusterName = name
		}
	}

	if clusterName == "" {
		return list
	}

	poolerList, ok := list.(*apiv1.PoolerList)
	if !ok {
		return list
	}

	var filteredPoolers []apiv1.Pooler
	for _, pooler := range poolerList.Items {
		if pooler.Spec.Cluster.Name == clusterName {
			filteredPoolers = append(filteredPoolers, pooler)
		}
	}

	poolerList.Items = filteredPoolers
	return poolerList
}

var _ = Describe("cluster_pooler_pause unit tests", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	Describe("pausePoolersDuringSwitchover", func() {
		It("should skip when feature is disabled", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)

			// Feature is disabled by default (Pooler is nil)
			err := env.clusterReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.PoolersPausedForSwitchover).To(BeFalse())
		})

		It("should skip when feature is explicitly disabled", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(false),
				}
			})

			err := env.clusterReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.PoolersPausedForSwitchover).To(BeFalse())
		})

		It("should skip when already paused for switchover", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(true),
				}
				c.Status.PoolersPausedForSwitchover = true
			})

			err := env.clusterReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should skip poolers without automated integration", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(true),
				}
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			// Create a pooler with manual auth (not automated integration)
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-manual",
					Namespace: namespace,
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						AuthQuery: "SELECT custom_auth($1)", // Custom auth = not automated
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = crReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should not be paused
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should skip already manually paused poolers", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(true),
				}
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			// Create a pooler that is already paused (manually)
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-manual-pause",
					Namespace: namespace,
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						Paused: ptr.To(true), // Already paused
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			err = crReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should not have our annotation (we didn't pause it)
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
		})

		It("should pause eligible poolers and update cluster status", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(true),
				}
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			// Create an eligible pooler (automated integration, not paused)
			pooler := newFakePooler(env.client, cluster)

			err := crReconciler.pausePoolersDuringSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should be paused and have our annotation
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
			Expect(updatedPooler.Annotations[utils.PausedDuringSwitchoverAnnotationName]).To(Equal("true"))

			// Cluster status should be updated
			updatedCluster := &apiv1.Cluster{}
			err = env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: namespace}, updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.PoolersPausedForSwitchover).To(BeTrue())
			Expect(updatedCluster.Status.PoolersPausedTimestamp).ToNot(BeEmpty())
		})
	})

	Describe("resumePoolersAfterSwitchover", func() {
		It("should skip when poolers were not paused by us", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)

			// PoolersPausedForSwitchover is false by default
			err := env.clusterReconciler.resumePoolersAfterSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should only resume poolers with our annotation", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover: ptr.To(true),
				}
				c.Status.PoolersPausedForSwitchover = true
				c.Status.PoolersPausedTimestamp = pgTime.GetCurrentTimestamp()
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			// Create a pooler paused by us (has annotation)
			poolerAuto := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-auto-paused",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: "true",
					},
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						Paused: ptr.To(true),
					},
				},
			}
			err := env.client.Create(ctx, poolerAuto)
			Expect(err).ToNot(HaveOccurred())

			// Create a pooler paused manually (no annotation)
			poolerManual := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-manual-paused",
					Namespace: namespace,
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						Paused: ptr.To(true),
					},
				},
			}
			err = env.client.Create(ctx, poolerManual)
			Expect(err).ToNot(HaveOccurred())

			err = crReconciler.resumePoolersAfterSwitchover(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Auto-paused pooler should be resumed
			updatedPoolerAuto := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: poolerAuto.Name, Namespace: namespace}, updatedPoolerAuto)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPoolerAuto.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPoolerAuto.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))

			// Manual-paused pooler should remain paused
			updatedPoolerManual := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: poolerManual.Name, Namespace: namespace}, updatedPoolerManual)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPoolerManual.Spec.PgBouncer.IsPaused()).To(BeTrue())

			// Cluster status should be updated
			updatedCluster := &apiv1.Cluster{}
			err = env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: namespace}, updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.PoolersPausedForSwitchover).To(BeFalse())
			Expect(updatedCluster.Status.PoolersPausedTimestamp).To(BeEmpty())
		})
	})

	Describe("checkPoolerPauseTimeout", func() {
		It("should skip when poolers are not paused", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)

			err := env.clusterReconciler.checkPoolerPauseTimeout(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not resume before timeout", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover:        ptr.To(true),
					PauseDuringSwitchoverTimeout: &metav1.Duration{Duration: 5 * time.Minute},
				}
				c.Status.PoolersPausedForSwitchover = true
				// Pause just happened (current timestamp)
				c.Status.PoolersPausedTimestamp = pgTime.GetCurrentTimestamp()
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			err := crReconciler.checkPoolerPauseTimeout(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Status should remain unchanged (still paused)
			Expect(cluster.Status.PoolersPausedForSwitchover).To(BeTrue())
		})

		It("should force resume after timeout exceeded", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)

			// Set timestamp to 10 minutes ago using RFC3339Micro format (same as pgTime.GetCurrentTimestamp)
			const rfc3339Micro = "2006-01-02T15:04:05.000000Z07:00"
			oldTimestamp := time.Now().Add(-10 * time.Minute).Format(rfc3339Micro)

			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Spec.Pooler = &apiv1.PoolerIntegrationConfiguration{
					PauseDuringSwitchover:        ptr.To(true),
					PauseDuringSwitchoverTimeout: &metav1.Duration{Duration: 5 * time.Minute},
				}
				c.Status.PoolersPausedForSwitchover = true
				c.Status.PoolersPausedTimestamp = oldTimestamp
			})

			// Create reconciler with index adapter
			crReconciler := &ClusterReconciler{
				Client: fakeClientWithIndexAdapter{
					Client:          env.clusterReconciler.Client,
					indexerAdapters: []indexAdapter{poolerClusterKeyIndexAdapter},
				},
				DiscoveryClient: env.clusterReconciler.DiscoveryClient,
				Scheme:          env.clusterReconciler.Scheme,
				Recorder:        record.NewFakeRecorder(120),
			}

			// Create a pooler that was auto-paused
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-timeout-test",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: "true",
					},
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						Paused: ptr.To(true),
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			// Manually test the timeout check logic - verify cluster status indicates timeout exceeded
			pauseDuration, parseErr := pgTime.DifferenceBetweenTimestamps(
				pgTime.GetCurrentTimestamp(),
				cluster.Status.PoolersPausedTimestamp,
			)
			Expect(parseErr).ToNot(HaveOccurred())
			Expect(pauseDuration).To(BeNumerically(">=", 5*time.Minute))

			err = crReconciler.checkPoolerPauseTimeout(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should be resumed (verify via Get from the same client the reconciler uses)
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))

			// Cluster status should be updated
			updatedCluster := &apiv1.Cluster{}
			err = env.client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: namespace}, updatedCluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedCluster.Status.PoolersPausedForSwitchover).To(BeFalse())
			Expect(updatedCluster.Status.PoolersPausedTimestamp).To(BeEmpty())
		})
	})

	Describe("helper functions", func() {
		It("should pause and resume a pooler correctly", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)

			// Create a pooler
			pooler := newFakePooler(env.client, cluster)

			// Verify initial state
			Expect(pooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(pooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))

			// Pause the pooler
			err := env.clusterReconciler.pausePooler(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			// Verify paused state
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeTrue())
			Expect(updatedPooler.Annotations[utils.PausedDuringSwitchoverAnnotationName]).To(Equal("true"))

			// Resume the pooler
			err = env.clusterReconciler.resumePooler(ctx, updatedPooler)
			Expect(err).ToNot(HaveOccurred())

			// Verify resumed state
			finalPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, finalPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(finalPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(finalPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
		})
	})
})
