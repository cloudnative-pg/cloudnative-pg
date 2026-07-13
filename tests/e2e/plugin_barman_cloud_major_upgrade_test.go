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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin counterpart of the in-core "PostgreSQL system" major-upgrade scenario,
// which is the only one wiring up object-store archiving: it verifies that WAL
// archiving keeps working after a Postgres major version upgrade. Here the
// archiver is plugin-barman-cloud rather than the in-core barmanObjectStore. The
// in-core variant (and the other upgrade scenarios it covers) is left in place.
var _ = Describe("plugin-barman-cloud across a Postgres major upgrade",
	Label(tests.LabelPluginBarmanCloud, tests.LabelPostgresMajorUpgrade, tests.LabelBackupRestore), func() {
		const (
			level      = tests.Medium
			pluginName = "barman-cloud.cloudnative-pg.io"
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if IsOpenshift() {
				Skip("This test case is not applicable on OpenShift clusters")
			}
		})

		It("keeps WAL archiving working across a Postgres major upgrade", func() {
			const (
				namespacePrefix = "plugin-major-upgrade"
				clusterName     = "pg-major-upgrade-plugin"
			)

			// Upgrade from one major behind the operator's default to the default.
			// The target major comes from versions.DefaultImageName; the starting
			// major is the env image's major when it is older, otherwise the major
			// right before the target, so there is always a real upgrade to run.
			// Starting images come from the official registry, the target from the
			// (possibly overridden) test one, as in cluster_major_upgrade_test.go.
			targetVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
			Expect(err).ToNot(HaveOccurred())
			targetMajor := targetVersion.Major()
			targetTag := strconv.FormatUint(targetMajor, 10)

			currentVersion, err := version.FromTag(env.PostgresImageTag)
			Expect(err).ToNot(HaveOccurred())
			startMajor := currentVersion.Major()

			switch {
			case startMajor == targetMajor:
				startMajor = targetMajor - 1
			case startMajor > targetMajor:
				// env.PostgresImageTag is a not-yet-released beta ahead of the
				// operator's default: upgrade to it instead of ignoring it, keeping
				// its raw tag (e.g. "19beta1") since beta images aren't published
				// under a plain major-number tag.
				startMajor, targetMajor = targetMajor, startMajor
				targetTag = strings.Split(env.PostgresImageTag, "-")[0]
			}

			startImage := env.OfficialMinimalImageName(strconv.FormatUint(startMajor, 10))
			targetImage := env.MinimalImageName(targetTag)

			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, clusterName)

			storageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
			Expect(storageClass).ToNot(BeEmpty())

			By("creating a cluster on the starting major that archives through the plugin", func() {
				cluster := &apiv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
					Spec: apiv1.ClusterSpec{
						Instances: 2,
						ImageName: startImage,
						Bootstrap: &apiv1.BootstrapConfiguration{
							InitDB: &apiv1.BootstrapInitDB{
								PostInitSQL: []string{
									"CREATE EXTENSION IF NOT EXISTS pg_stat_statements;",
									"CREATE EXTENSION IF NOT EXISTS pg_trgm;",
								},
								DataChecksums:  ptr.To(true),
								WalSegmentSize: 32,
							},
						},
						StorageConfiguration: apiv1.StorageConfiguration{
							StorageClass: &storageClass,
							Size:         "1Gi",
						},
						WalStorage: &apiv1.StorageConfiguration{
							StorageClass: &storageClass,
							Size:         "1Gi",
						},
						PostgresConfiguration: apiv1.PostgresConfiguration{
							Parameters: map[string]string{
								"log_checkpoints":             "on",
								"log_lock_waits":              "on",
								"log_min_duration_statement":  "1000",
								"log_statement":               "ddl",
								"log_temp_files":              "1024",
								"log_autovacuum_min_duration": "1000",
								"log_replication_commands":    "on",
								"pg_stat_statements.track":    "top",
							},
						},
						Plugins: []apiv1.PluginConfiguration{
							{
								Name:          pluginName,
								IsWALArchiver: ptr.To(true),
								Parameters:    map[string]string{"barmanObjectName": clusterName},
							},
						},
					},
				}
				clusterutils.AddTopologySpreadConstraint(cluster)
				Expect(env.Client.Create(env.Ctx, cluster)).To(Succeed())
				clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
			})

			By("verifying WAL archiving through the plugin is working before the upgrade", func() {
				backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
			})

			primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			var oldVersion string
			By("checking the cluster starts on the older major version", func() {
				stdOut, stdErr, err := exec.EventuallyExecQueryInInstancePod(env.Ctx, env.Client, env.Interface,
					env.RestClientConfig,
					exec.PodLocator{Namespace: primary.GetNamespace(), PodName: primary.GetName()}, postgres.AppDBName,
					"SELECT version();", 60, objects.PollingTime)
				Expect(err).ToNot(HaveOccurred())
				Expect(stdErr).To(BeEmpty())
				Expect(stdOut).To(ContainSubstring(strconv.FormatUint(startMajor, 10)))
				oldVersion = stdOut
			})

			By("triggering the major upgrade by updating the image", func() {
				Eventually(func() error {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return err
					}
					cluster.Spec.ImageName = targetImage
					return env.Client.Update(env.Ctx, cluster)
				}).WithTimeout(1*time.Minute).WithPolling(10*time.Second).Should(
					Succeed(),
					"Failed to update cluster image from %s to %s",
					startImage,
					targetImage,
				)
			})

			By("waiting for the cluster to enter the major upgrade phase", func() {
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseMajorUpgrade))
				}).WithTimeout(1 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("waiting for the upgraded cluster to become ready", func() {
				clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])
			})

			By("verifying the cluster now runs the target major version", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(func(g Gomega) {
					stdOut, stdErr, err := exec.EventuallyExecQueryInInstancePod(env.Ctx, env.Client, env.Interface,
						env.RestClientConfig,
						exec.PodLocator{Namespace: primary.GetNamespace(), PodName: primary.GetName()}, postgres.AppDBName,
						"SELECT version();", 60, objects.PollingTime)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(stdErr).To(BeEmpty())
					g.Expect(stdOut).ToNot(Equal(oldVersion))
					g.Expect(stdOut).To(ContainSubstring(strconv.FormatUint(targetMajor, 10)))
				}, 120).Should(Succeed())
			})

			By("verifying WAL archiving through the plugin still works after the upgrade", func() {
				objectstoreasserts.AssertArchiveWalOnObjectStore(env, testTimeouts, objectStoreEnv,
					namespace, clusterName, clusterName)
			})
		})
	})
