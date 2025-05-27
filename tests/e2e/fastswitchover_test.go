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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fast switchover", Serial, Label(tests.LabelPerformance, tests.LabelSelfHealing), func() {
	const (
		sampleFileWithReplicationSlots = fixturesDir +
			"/fastswitchover/cluster-fast-switchover-with-repl-slots.yaml.template"
		sampleFileWithoutReplicationSlots = fixturesDir + "/fastswitchover/cluster-fast-switchover.yaml.template"
		webTestFile                       = fixturesDir + "/fastswitchover/webtest.yaml"
		webTestJob                        = fixturesDir + "/fastswitchover/apache-benchmark-webtest.yaml"
		clusterName                       = "cluster-fast-switchover"
		level                             = tests.Highest
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	// Confirm that a standby closely following the primary doesn't need more
	// than maxSwitchoverTime seconds to be promoted and be able to start
	// inserting records. We then expect the old primary to be back in
	// maxReattachTime.
	// We test this setting up an application pointing to the rw service,
	// forcing a switchover and measuring how much time passes between the
	// last row written on timeline 1 and the first one on timeline 2
	Context("without HA Replication Slots", func() {
		It("can do a fast switchover", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "primary-switchover-time"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			assertFastSwitchover(namespace, sampleFileWithoutReplicationSlots, clusterName, webTestFile, webTestJob)
		})
	})
	Context("with HA Replication Slots", func() {
		It("can do a fast switchover", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "primary-switchover-time-with-slots"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			assertFastSwitchover(namespace, sampleFileWithReplicationSlots, clusterName, webTestFile, webTestJob)
			AssertClusterHAReplicationSlots(namespace, clusterName)
		})
	})
})

func assertFastSwitchover(namespace, sampleFile, clusterName, webTestFile, webTestJob string) {
	var oldPrimary, targetPrimary string

	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})
	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		CreateResourceFromFile(namespace, sampleFile)
	})
	By("having a Cluster with three instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
	})
	// Node 1 should be the primary, so the -rw service should
	// point there. We verify this.
	By("having the current primary on node1", func() {
		rwServiceName := clusterName + "-rw"
		endpointSlice, err := utils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, rwServiceName)
		Expect(err).ToNot(HaveOccurred())

		oldPrimary = clusterName + "-1"
		pod := &corev1.Pod{}
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: oldPrimary}, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(utils.FirstEndpointSliceIP(endpointSlice)).To(BeEquivalentTo(pod.Status.PodIP))
	})
	By("preparing the db for the test scenario", func() {
		// Create the table used by the scenario
		query := "CREATE SCHEMA IF NOT EXISTS tps; " +
			"CREATE TABLE IF NOT EXISTS tps.tl ( " +
			"id BIGSERIAL" +
			", timeline TEXT DEFAULT (substring(pg_walfile_name(" +
			"    pg_current_wal_lsn()), 1, 8))" +
			", t timestamp DEFAULT (clock_timestamp() AT TIME ZONE 'UTC')" +
			", source text NOT NULL" +
			", PRIMARY KEY (id)" +
			")"

		_, err := postgres.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		// We set up Apache Benchmark and webtest. Apache Benchmark, a load generator,
		// continuously calls the webtest api to execute inserts
		// on the postgres primary. We make sure that the first
		// records appear on the database before moving to the next
		// step.
		_, _, err := run.Run("kubectl create -n " + namespace +
			" -f " + webTestFile)
		Expect(err).ToNot(HaveOccurred())

		webtestDeploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "webtest", Namespace: namespace}}
		Expect(deployments.WaitForReady(env.Ctx, env.Client, webtestDeploy, 60)).To(Succeed())

		_, _, err = run.Run("kubectl create -n " + namespace +
			" -f " + webTestJob)
		Expect(err).ToNot(HaveOccurred())

		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      oldPrimary,
		}
		query := "SELECT count(*) > 0 FROM tps.tl"
		Eventually(func() (string, error) {
			primaryPod := &corev1.Pod{}
			err := env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
			if err != nil {
				return "", err
			}
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.AppDBName,
				query)
			return strings.TrimSpace(out), err
		}, RetryTimeout).Should(BeEquivalentTo("t"))
	})

	By("setting the TargetPrimary to node2 to trigger a switchover", func() {
		targetPrimary = clusterName + "-2"
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Status.TargetPrimary = targetPrimary
			return env.Client.Status().Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	})

	var maxReattachTime int32 = 60
	var maxSwitchoverTime int32 = 20

	AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(namespace, clusterName, maxSwitchoverTime)
}
