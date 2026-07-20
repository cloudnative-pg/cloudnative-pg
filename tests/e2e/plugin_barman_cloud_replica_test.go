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
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	replicationasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Plugin port of the "bootstrap a replica cluster from a backup" scenario: a
// source cluster archives through plugin-barman-cloud, then a replica cluster
// is bootstrapped from that backup via externalClusters[].plugin and streams
// from the source. It is added alongside the in-core variant (which shares its
// source cluster with a volume-snapshot test), and stays until the in-core
// Barman Cloud support is removed.
var _ = Describe("plugin-barman-cloud replica cluster from backup",
	Label(tests.LabelPluginBarmanCloud, tests.LabelReplication, tests.LabelBackupRestore), func() {
		const (
			srcManifest     = fixturesDir + "/replica_mode_cluster/cluster-replica-src-with-plugin.yaml.template"
			replicaManifest = fixturesDir + "/replica_mode_cluster/cluster-replica-from-plugin-backup.yaml.template"
			backupManifest  = fixturesDir + "/replica_mode_cluster/backup-cluster-replica-src-plugin.yaml"
			srcClusterName  = "cluster-replica-src-plugin"
			srcDBName       = "appSrc"
			testTableName   = "replica_mode_backup"
			level           = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		It("bootstraps a replica cluster from a plugin backup", func() {
			const namespacePrefix = "replica-cluster-from-plugin-backup"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, srcClusterName)

			By("creating the source cluster that archives through the plugin", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcManifest)
			})

			By("taking a backup of the source through the plugin", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupManifest, false,
					testTimeouts[timeouts.BackupIsReady])
			})

			By("bootstrapping a replica cluster from the plugin backup", func() {
				replicationasserts.AssertReplicaModeCluster(env, testTimeouts, namespace,
					srcClusterName, srcDBName, replicaManifest, testTableName)
			})
		})
	})
