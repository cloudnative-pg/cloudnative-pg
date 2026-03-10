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

package e2e

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod selector refs for pg_hba", Label(tests.LabelPostgresConfiguration), func() {
	const (
		clusterName = "pg-pod-selector-refs"
		level       = tests.Medium
	)
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("Dynamic pg_hba address resolution via podSelectorRefs", Ordered, func() {
		const appLabelKey = "app"
		const appLabelValue = "myapp-client"

		BeforeAll(func() {
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, "pod-selector-refs-e2e")
			Expect(err).ToNot(HaveOccurred())

			storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
			Expect(storageClass).ToNot(BeEmpty())

			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: apiv1.ClusterSpec{
					Instances: 1,
					StorageConfiguration: apiv1.StorageConfiguration{
						StorageClass: &storageClass,
						Size:         "1Gi",
					},
					PodSelectorRefs: []apiv1.PodSelectorRef{
						{
							Name: "app-clients",
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{appLabelKey: appLabelValue},
							},
						},
					},
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"log_checkpoints":            "on",
							"log_lock_waits":             "on",
							"log_replication_commands":   "on",
							"log_min_duration_statement": "1000",
						},
						PgHBA: []string{
							"hostssl all all ${podselector:app-clients} scram-sha-256",
						},
					},
				},
			}
			err = env.Client.Create(env.Ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
		})

		It("resolves pod IPs in cluster status and expands pg_hba rules", func() {
			By("verifying status has no IPs before creating matching pods", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
					g.Expect(cluster.Status.PodSelectorRefs[0].Name).To(Equal("app-clients"))
					g.Expect(cluster.Status.PodSelectorRefs[0].IPs).To(BeEmpty())
				}, RetryTimeout).Should(Succeed())
			})

			By("verifying pg_hba has no expanded rules (line is omitted when no pods match)", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))

				// With no matching pods, the ${podselector:app-clients} line should be omitted.
				// Verify no hostssl rules referencing our app-clients exist.
				query := "SELECT count(*) FROM pg_catalog.pg_hba_file_rules " +
					"WHERE type = 'hostssl' AND auth_method = 'scram-sha-256' " +
					"AND address IS NOT NULL AND netmask IS NOT NULL"
				Eventually(QueryMatchExpectationPredicate(&podList.Items[0], postgres.PostgresDBName,
					query, "0"), RetryTimeout).Should(Succeed())
			})

			By("creating matching pods", func() {
				for i := 1; i <= 2; i++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("app-client-%d", i),
							Namespace: namespace,
							Labels:    map[string]string{appLabelKey: appLabelValue},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "pause",
									Image:   "registry.k8s.io/pause:3.9",
									Command: []string{"/pause"},
								},
							},
						},
					}
					err := env.Client.Create(env.Ctx, pod)
					Expect(err).ToNot(HaveOccurred())
				}
			})

			var resolvedIPs []string
			By("verifying cluster status contains resolved pod IPs", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
					g.Expect(cluster.Status.PodSelectorRefs[0].Name).To(Equal("app-clients"))
					g.Expect(cluster.Status.PodSelectorRefs[0].IPs).To(HaveLen(2))

					resolvedIPs = cluster.Status.PodSelectorRefs[0].IPs
				}, RetryTimeout).Should(Succeed())
			})

			By("verifying pg_hba.conf contains expanded rules with /32 per pod IP", func() {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(podList.Items).To(HaveLen(1))
				pod := &podList.Items[0]

				// Query pg_hba_file_rules to check expanded lines exist
				expectedCount := fmt.Sprintf("%d", len(resolvedIPs))
				query := "SELECT count(*) FROM pg_catalog.pg_hba_file_rules " +
					"WHERE type = 'hostssl' AND auth_method = 'scram-sha-256' " +
					"AND address IS NOT NULL AND (netmask = '255.255.255.255' OR netmask = 'ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff')"
				Eventually(QueryMatchExpectationPredicate(pod, postgres.PostgresDBName,
					query, expectedCount), RetryTimeout).Should(Succeed())

				// Verify each pod IP appears in pg_hba_file_rules
				for _, ip := range resolvedIPs {
					ipQuery := fmt.Sprintf(
						"SELECT count(*) FROM pg_catalog.pg_hba_file_rules "+
							"WHERE type = 'hostssl' AND auth_method = 'scram-sha-256' "+
							"AND address = '%s'", ip)
					Eventually(QueryMatchExpectationPredicate(pod, postgres.PostgresDBName,
						ipQuery, "1"), RetryTimeout).Should(Succeed())
				}
			})

			By("verifying pg_hba updates when a matching pod is deleted", func() {
				// Delete one of the app pods
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-client-1",
						Namespace: namespace,
					},
				}
				err := env.Client.Delete(env.Ctx, pod)
				Expect(err).ToNot(HaveOccurred())

				// The cluster status should update to have only 1 IP
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
					g.Expect(cluster.Status.PodSelectorRefs[0].IPs).To(HaveLen(1))
				}, RetryTimeout).Should(Succeed())

				// pg_hba should now have only 1 expanded rule
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				instancePod := &podList.Items[0]
				query := "SELECT count(*) FROM pg_catalog.pg_hba_file_rules " +
					"WHERE type = 'hostssl' AND auth_method = 'scram-sha-256' " +
					"AND address IS NOT NULL AND (netmask = '255.255.255.255' OR netmask = 'ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff')"
				Eventually(QueryMatchExpectationPredicate(instancePod, postgres.PostgresDBName,
					query, "1"), RetryTimeout).Should(Succeed())
			})
		})

		It("updates pg_hba when podSelectorRefs are added to an existing cluster", func() {
			By("removing existing podSelectorRefs and pg_hba rules", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}
					cluster.Spec.PodSelectorRefs = nil
					cluster.Spec.PostgresConfiguration.PgHBA = nil
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying status is cleared", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.PodSelectorRefs).To(BeEmpty())
				}, RetryTimeout).Should(Succeed())
			})

			By("re-adding podSelectorRefs with a new pg_hba rule", func() {
				err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}
					cluster.Spec.PodSelectorRefs = []apiv1.PodSelectorRef{
						{
							Name: "app-clients",
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{appLabelKey: appLabelValue},
							},
						},
					}
					cluster.Spec.PostgresConfiguration.PgHBA = []string{
						"host all all ${podselector:app-clients} trust",
					}
					return env.Client.Update(env.Ctx, cluster)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying the new rule is expanded in pg_hba", func() {
				// app-client-2 should still be running
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.PodSelectorRefs).To(HaveLen(1))
					g.Expect(cluster.Status.PodSelectorRefs[0].IPs).To(HaveLen(1))
				}, RetryTimeout).Should(Succeed())

				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				instancePod := &podList.Items[0]

				query := "SELECT count(*) FROM pg_catalog.pg_hba_file_rules " +
					"WHERE type = 'host' AND auth_method = 'trust' " +
					"AND address IS NOT NULL AND (netmask = '255.255.255.255' OR netmask = 'ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff')"
				Eventually(QueryMatchExpectationPredicate(instancePod, postgres.PostgresDBName,
					query, "1"), RetryTimeout).Should(Succeed())
			})
		})
	})
})
