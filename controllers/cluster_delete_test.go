/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
)

var _ = Describe("ensures that deleteDanglingMonitoringQueries works correctly", func() {
	const cmName = apiv1.DefaultMonitoringConfigMapName

	BeforeEach(func() {
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.MonitoringQueriesConfigmap = cmName
	})

	It("should make sure that a dangling monitoring queries config map is deleted", func() {
		withManager(func(ctx context.Context, crReconciler *ClusterReconciler, poolerReconciler *PoolerReconciler,
			manager manager.Manager,
		) {
			namespace := newFakeNamespace()

			By("creating the required monitoring configmap", func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: namespace,
					},
					BinaryData: map[string][]byte{},
				}
				err := crReconciler.Create(ctx, cm)
				Expect(err).ToNot(HaveOccurred())
			})

			assertRefreshManagerCache(ctx, manager)

			By("making sure configmap exists", func() {
				cm := &corev1.ConfigMap{}
				expectResourceExistsWithDefaultClient(cmName, namespace, cm)
			})

			By("deleting the dangling monitoring configmap", func() {
				err := crReconciler.deleteDanglingMonitoringQueries(ctx, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			assertRefreshManagerCache(ctx, manager)

			By("making sure it doesn't exist anymore", func() {
				expectResourceDoesntExistWithDefaultClient(cmName, namespace, &corev1.ConfigMap{})
			})
		})
	})

	It("should make sure that the configmap is not deleted if a cluster is running", func() {
		withManager(func(ctx context.Context, crReconciler *ClusterReconciler, poolerReconciler *PoolerReconciler,
			manager manager.Manager,
		) {
			namespace := newFakeNamespace()
			var cluster *apiv1.Cluster

			By("creating the required monitoring configmap", func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: namespace,
					},
					BinaryData: map[string][]byte{},
				}
				err := crReconciler.Create(ctx, cm)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the required resources", func() {
				cluster = newFakeCNPGCluster(namespace)
			})

			assertRefreshManagerCache(ctx, manager)

			By("making sure that the configmap and the cluster exists", func() {
				expectResourceExists(crReconciler.Client, cmName, namespace, &corev1.ConfigMap{})
				expectResourceExists(crReconciler.Client, cluster.Name, namespace, &apiv1.Cluster{})
			})

			By("making sure that the cache is indexed", func() {
				Eventually(func(g Gomega) {
					clustersUsingDefaultMetrics := apiv1.ClusterList{}
					err := crReconciler.List(
						ctx,
						&clustersUsingDefaultMetrics,
						client.InNamespace(namespace),
						client.MatchingFields{disableDefaultQueriesSpecPath: "false"},
					)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(clustersUsingDefaultMetrics.Items).To(HaveLen(1))
				}, 20*time.Second).Should(Succeed())
			})

			By("deleting the dangling monitoring configmap", func() {
				err := crReconciler.deleteDanglingMonitoringQueries(ctx, namespace)
				Expect(err).ToNot(HaveOccurred())
			})

			assertRefreshManagerCache(ctx, manager)

			By("making sure it still exists", func() {
				expectResourceExistsWithDefaultClient(cmName, namespace, &corev1.ConfigMap{})
			})
		})
	})
})
