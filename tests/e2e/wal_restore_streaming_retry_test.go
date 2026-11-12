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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// PostgreSQL moves a standby from the WAL archive to streaming replication only
// when restore_command fails. A standby whose archive always has the next WAL
// file it needs never streams, unless the instance manager makes the restore
// fail on purpose, before asking the plugin for the WAL file.
var _ = Describe("WAL restore streaming retry", Label(tests.LabelPluginBarmanCloud, tests.LabelReplication), func() {
	const (
		level         = tests.Medium
		sourceSample  = fixturesDir + "/wal_restore_streaming_retry/source.yaml.template"
		replicaSample = fixturesDir + "/wal_restore_streaming_retry/replica.yaml.template"
		sourceName    = "streaming-retry-source"
		replicaName   = "streaming-retry-replica"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("brings a standby that keeps restoring from the archive back to streaming", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, "streaming-retry")
		Expect(err).ToNot(HaveOccurred())

		// The archive must not fall behind the apply delay of the replica cluster,
		// which would then reach its end and stream without any forced failure
		setupPluginObjectStore(namespace, sourceName, func(objectStore *unstructured.Unstructured) {
			Expect(unstructured.SetNestedField(objectStore.Object, int64(4),
				"spec", "configuration", "wal", "maxParallel")).To(Succeed())
		})

		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, sourceName, sourceSample)
		// A lone instance can wait for archive_timeout before completing a WAL file
		objectstoreasserts.AssertArchiveWalOnObjectStore(env, testTimeouts, objectStoreEnv,
			namespace, sourceName, sourceName)
		backupasserts.AssertArchiveConditionMet(env, namespace, sourceName, testTimeouts[timeouts.BackupIsReady])

		source, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, sourceName)
		Expect(err).ToNot(HaveOccurred())
		sourceLocator := exec.PodLocator{Namespace: namespace, PodName: source.Name}
		_, _, err = exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			sourceLocator, postgres.AppDBName, "CREATE TABLE streaming_retry (ts timestamptz)")
		Expect(err).ToNot(HaveOccurred())

		By("writing a WAL file every second on the source", func() {
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(done)
				for {
					select {
					case <-stop:
						return
					case <-time.After(time.Second):
					}
					// A failed write is just one WAL file less
					_, _, _ = exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						sourceLocator, postgres.AppDBName,
						"INSERT INTO streaming_retry VALUES (now()); SELECT pg_switch_wal()")
				}
			}()
			DeferCleanup(func() {
				close(stop)
				<-done
			})
		})

		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, replicaName, replicaSample)
		designatedPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, replicaName)
		Expect(err).ToNot(HaveOccurred())

		By("restarting the designated primary, which resumes from the archive", func() {
			Expect(pods.Delete(env.Ctx, env.Client, namespace, designatedPrimary.Name)).To(Succeed())
			clusterasserts.AssertClusterIsReady(env, namespace, replicaName, testTimeouts[timeouts.ClusterIsReadyQuick])
		})

		By("waiting for the designated primary to stream from the source", func() {
			locator := exec.PodLocator{Namespace: namespace, PodName: designatedPrimary.Name}
			Eventually(func(g Gomega) {
				status, _, err := exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					locator, postgres.PostgresDBName, "SELECT status FROM pg_stat_wal_receiver")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(strings.TrimSpace(status)).To(Equal("streaming"))
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		})
	})
})
