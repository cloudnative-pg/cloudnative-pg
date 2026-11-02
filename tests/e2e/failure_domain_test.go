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
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Verifies that .spec.postgresql.synchronous.nodeFailureDomainKeys steers
// synchronous replica election towards standbys running on a Node in a
// different failure domain than the primary, and that the
// SyncReplicationTopologySatisfied condition reflects whether that constraint
// can currently be honored.
//
// The failure domains are built by this test, labelling the Node of a single
// standby with a key of its own: relying on the topology labels of the
// underlying cluster would make the outcome depend on where the scheduler
// happened to put the pods, and on a cluster whose every Node sits in a
// distinct zone no configuration can leave the constraint unsatisfiable.
var _ = Describe("Failure domain-aware synchronous replication", Label(tests.LabelReplication), func() {
	const (
		level             = tests.Medium
		namespacePrefix   = "failure-domain-e2e"
		sampleFile        = fixturesDir + "/failure_domain/cluster-failure-domain.yaml.template"
		failureDomainKey  = "e2e.cnpg.io/failure-domain"
		remoteDomainValue = "remote"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	assertSyncTopologyCondition := func(namespace, clusterName string, status metav1.ConditionStatus, reason string) {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			cond := meta.FindStatusCondition(
				cluster.Status.Conditions,
				string(apiv1.ConditionSyncReplicationTopologySatisfied),
			)
			g.Expect(cond).ToNot(BeNil())
			g.Expect(cond.Status).To(Equal(status))
			g.Expect(cond.Reason).To(Equal(reason))
		}, environment.RetryTimeout).Should(Succeed())
	}

	// assertSynchronousStandbyNames waits for synchronous_standby_names on the
	// primary to contain every name in included and none of the names in
	// excluded.
	assertSynchronousStandbyNames := func(namespace, clusterName string, included, excluded []string) {
		Eventually(func(g Gomega) {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			out, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primaryPod.GetName(),
				},
				"postgres",
				"select setting from pg_catalog.pg_settings where name = 'synchronous_standby_names'")
			g.Expect(stdErr).To(BeEmpty())
			g.Expect(err).ToNot(HaveOccurred())

			setting := strings.Trim(out, "\n")
			for _, name := range included {
				g.Expect(setting).To(ContainSubstring(fmt.Sprintf("%q", name)))
			}
			for _, name := range excluded {
				g.Expect(setting).ToNot(ContainSubstring(fmt.Sprintf("%q", name)))
			}
		}, environment.RetryTimeout).Should(Succeed())
	}

	updateCluster := func(namespace, clusterName string, mutate func(*apiv1.Cluster)) {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			mutate(cluster)
			g.Expect(env.Client.Update(env.Ctx, cluster)).To(Succeed())
		}, environment.RetryTimeout).Should(Succeed())
	}

	It("elects only cross-domain standbys, and gives up when they are not enough", func() {
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
		Expect(err).ToNot(HaveOccurred())

		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(podList.Items).To(HaveLen(3))

		podsPerNode := make(map[string][]string)
		for i := range podList.Items {
			pod := &podList.Items[i]
			podsPerNode[pod.Spec.NodeName] = append(podsPerNode[pod.Spec.NodeName], pod.Name)
		}

		// the standby moved to its own failure domain has to be alone on its
		// Node, or labelling that Node would move the primary or the other
		// standby along with it
		var remoteStandby, remoteNode string
		for node, pods := range podsPerNode {
			if len(pods) == 1 && pods[0] != primaryPod.Name {
				remoteStandby, remoteNode = pods[0], node
				break
			}
		}
		if remoteStandby == "" {
			Skip("no Node hosts exactly one standby: the failure domains of this test cannot be built")
		}

		var standbyNames []string
		for i := range podList.Items {
			if name := podList.Items[i].Name; name != primaryPod.Name {
				standbyNames = append(standbyNames, name)
			}
		}
		sort.Strings(standbyNames)
		localStandbys := []string{}
		for _, name := range standbyNames {
			if name != remoteStandby {
				localStandbys = append(localStandbys, name)
			}
		}

		By(fmt.Sprintf("moving %v to its own failure domain", remoteStandby), func() {
			var node corev1.Node
			Expect(env.Client.Get(env.Ctx, client.ObjectKey{Name: remoteNode}, &node)).To(Succeed())
			updated := node.DeepCopy()
			if updated.Labels == nil {
				updated.Labels = make(map[string]string)
			}
			updated.Labels[failureDomainKey] = remoteDomainValue
			Expect(env.Client.Patch(env.Ctx, updated, client.MergeFrom(&node))).To(Succeed())

			DeferCleanup(func() error {
				var node corev1.Node
				if err := env.Client.Get(env.Ctx, client.ObjectKey{Name: remoteNode}, &node); err != nil {
					return err
				}
				updated := node.DeepCopy()
				delete(updated.Labels, failureDomainKey)
				return env.Client.Patch(env.Ctx, updated, client.MergeFrom(&node))
			})
		})

		By("requesting the failure domain-aware election", func() {
			updateCluster(namespace, clusterName, func(cluster *apiv1.Cluster) {
				cluster.Spec.PostgresConfiguration.Synchronous.NodeFailureDomainKeys = []string{failureDomainKey}
			})
			assertSyncTopologyCondition(namespace, clusterName,
				metav1.ConditionTrue, string(apiv1.ConditionReasonTopologySatisfied))
		})

		By("verifying synchronous_standby_names lists the cross-domain standby alone", func() {
			assertSynchronousStandbyNames(namespace, clusterName, []string{remoteStandby}, localStandbys)
		})

		By("requiring more synchronous replicas than the cross-domain standbys available", func() {
			updateCluster(namespace, clusterName, func(cluster *apiv1.Cluster) {
				cluster.Spec.PostgresConfiguration.Synchronous.Number = 2
			})
			assertSyncTopologyCondition(namespace, clusterName,
				metav1.ConditionFalse, string(apiv1.ConditionReasonInsufficientCrossDomainReplicas))
		})

		By("verifying synchronous_standby_names falls back to every standby", func() {
			assertSynchronousStandbyNames(namespace, clusterName, standbyNames, nil)
		})

		By("verifying writes still succeed on the primary", func() {
			commandTimeout := 10 * time.Second
			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = exec.Command(
				env.Ctx, env.Interface, env.RestClientConfig,
				*primary, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "-c",
				"create table failure_domain_write_check (i int); insert into failure_domain_write_check values (1);",
			)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
