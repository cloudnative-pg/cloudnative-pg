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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin port of the "bootstrap a replica cluster from a backup" scenario: a
// source cluster archives through plugin-barman-cloud, then a replica cluster
// is bootstrapped from that backup via externalClusters[].plugin and streams
// from the source. It is added alongside the in-core variant (which shares its
// source cluster with a volume-snapshot test), and stays until the in-core
// Barman Cloud support is removed. Runs on kind/k3d only, where the plugin and
// the shared MinIO are installed.
var _ = Describe("plugin-barman-cloud replica cluster from backup",
	Label(tests.LabelPlugin, tests.LabelReplication, tests.LabelBackupRestore), func() {
		const (
			srcManifest           = fixturesDir + "/replica_mode_cluster/cluster-replica-src-with-plugin.yaml.template"
			replicaManifest       = fixturesDir + "/replica_mode_cluster/cluster-replica-from-plugin-backup.yaml.template"
			srcClusterName        = "cluster-replica-src-plugin"
			srcDBName             = "appSrc"
			testTableName         = "replica_mode_backup"
			barmanCloudPluginName = "barman-cloud.cloudnative-pg.io"
			level                 = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
		})

		It("bootstraps a replica cluster from a plugin backup", func() {
			const namespacePrefix = "replica-cluster-from-plugin-backup"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			By("creating the MinIO CA secret", func() {
				Expect(minioEnv.CreateCaSecret(env, namespace)).To(Succeed())
			})

			By("creating the MinIO credentials secret", func() {
				_, err := secrets.CreateObjectStorageSecret(
					env.Ctx, env.Client, namespace, barmanCloudCredentialSecret, "minio", "minio123")
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the ObjectStore pointing at the shared MinIO", func() {
				Expect(env.Client.Create(env.Ctx, newMinioObjectStore(namespace, srcClusterName))).
					To(Succeed())
			})

			By("creating the source cluster that archives through the plugin", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcManifest)
			})

			By("taking a backup of the source through the plugin", func() {
				backupName := fmt.Sprintf("%v-backup", srcClusterName)
				backup, err := backups.Create(env.Ctx, env.Client, apiv1.Backup{
					ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: backupName},
					Spec: apiv1.BackupSpec{
						Target:              apiv1.BackupTargetStandby,
						Method:              apiv1.BackupMethodPlugin,
						PluginConfiguration: &apiv1.BackupPluginConfiguration{Name: barmanCloudPluginName},
						Cluster:             apiv1.LocalObjectReference{Name: srcClusterName},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() (apiv1.BackupPhase, error) {
					err = env.Client.Get(env.Ctx,
						types.NamespacedName{Namespace: namespace, Name: backupName}, backup)
					return backup.Status.Phase, err
				}, testTimeouts[timeouts.BackupIsReady]).Should(BeEquivalentTo(apiv1.BackupPhaseCompleted))
			})

			By("bootstrapping a replica cluster from the plugin backup", func() {
				replicationasserts.AssertReplicaModeCluster(env, testTimeouts, namespace,
					srcClusterName, srcDBName, replicaManifest, testTableName)
			})
		})
	})
