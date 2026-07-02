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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	backupasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/backup"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This is a minimal smoke test that exercises the plugin-barman-cloud build
// selected for the run (see BARMAN_PLUGIN_VERSION / the e2e workflow selector):
// it confirms the plugin is installed, loaded by the operator, and that a
// cluster can both back up to and recover from an object store through it. It
// is a stopgap that the fuller plugin-barman-cloud backup/restore ports will
// absorb or replace.
var _ = Describe("plugin-barman-cloud",
	Label(tests.LabelSmoke, tests.LabelPluginBarmanCloud, tests.LabelBackupRestore), func() {
		const (
			clusterManifest = fixturesDir + "/plugin_barman_cloud/cluster-with-plugin.yaml.template"
			backupManifest  = fixturesDir + "/plugin_barman_cloud/backup-plugin.yaml"
			restoreManifest = fixturesDir + "/plugin_barman_cloud/cluster-restore-plugin.yaml.template"
			// Matches metadata.name of the cluster fixture and the barmanObjectName
			// parameter it references.
			clusterName           = "pg-backup-plugin"
			barmanCloudPluginName = "barman-cloud.cloudnative-pg.io"
			restoreTable          = "to_restore"
			level                 = tests.High
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
			// The plugin (and the shared object store it backs up to) is only
			// installed on local kind/k3d engines; see hack/e2e/run-e2e.sh.
			if !(IsKind() || IsK3D()) {
				Skip("This test only runs on kind or k3d clusters")
			}
		})

		It("backs up and restores a cluster through the selected plugin-barman-cloud", func() {
			const namespacePrefix = "plugin-barman-cloud-smoke"
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			setupPluginObjectStore(namespace, clusterName)

			By("creating a cluster that uses the plugin as WAL archiver", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
			})

			By("verifying the plugin is loaded and reported in the cluster status", func() {
				clusterasserts.AssertPluginLoaded(env, namespace, clusterName, barmanCloudPluginName, 120)
			})

			By("verifying WAL archiving through the plugin is working", func() {
				// Fail early and descriptively if the plugin is loaded but cannot
				// archive WALs, rather than later as an opaque backup timeout.
				backupasserts.AssertArchiveConditionMet(env, namespace, clusterName, 120)
			})

			// Write known data so the restore can be verified end to end.
			pgasserts.AssertCreateTestData(env, pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  clusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    restoreTable,
			})

			By("backing up the cluster through the plugin", func() {
				backups.Execute(env.Ctx, env.Client, env.Scheme, namespace, backupManifest, false,
					testTimeouts[timeouts.BackupIsReady])
				backupasserts.AssertBackupConditionInClusterStatus(env, namespace, clusterName)
			})

			assertArchiveWalClosingPluginBackup(namespace, clusterName)

			// Recover into a new cluster from the backup and confirm the data is
			// there, exercising the plugin's restore path end to end.
			backupasserts.AssertClusterRestore(env, testTimeouts, namespace, restoreManifest, restoreTable)
		})
	})

// assertArchiveWalClosingPluginBackup switches and archives the WAL segment
// that closes a plugin backup. Without this, the segment holding the
// backup-stop record can stay unarchived until archive_timeout (CNPG's
// default: 5 minutes) elapses on its own, and a restore attempted before then
// fails with "WAL ends before consistent recovery point".
func assertArchiveWalClosingPluginBackup(namespace, clusterName string) {
	GinkgoHelper()
	By("archiving the WAL that closes the backup", func() {
		objectstoreasserts.AssertArchiveWalOnObjectStore(
			env, testTimeouts, objectStoreEnv, namespace, clusterName, clusterName)
	})
}

// barmanCloudCredentialSecret is the Secret holding the object store credentials
// that the e2e plugin ObjectStores reference; the tests create it with keys ID/KEY.
const barmanCloudCredentialSecret = "backup-storage-creds"

// setupPluginObjectStore prepares a namespace for plugin-barman-cloud
// archiving against the shared object store: the CA secret, the credentials
// secret, and an ObjectStore named after the cluster that uses them.
// Optional mutators can be passed to customize the ObjectStore spec before
// it is created (e.g. to enable immediateCheckpoint).
func setupPluginObjectStore(namespace, name string, mutators ...func(*unstructured.Unstructured)) {
	GinkgoHelper()

	By("creating the object store CA secret", func() {
		Expect(objectStoreEnv.CreateCaSecret(env, namespace)).To(Succeed())
	})

	By("creating the object store credentials secret", func() {
		_, err := secrets.CreateObjectStorageSecret(
			env.Ctx, env.Client, namespace, barmanCloudCredentialSecret,
			objectstore.AccessKeyID, objectstore.SecretAccessKey)
		Expect(err).ToNot(HaveOccurred())
	})

	By("creating the ObjectStore pointing at the shared object store", func() {
		Expect(env.Client.Create(env.Ctx, newObjectStore(namespace, name, mutators...))).To(Succeed())
	})
}

// newObjectStore builds a plugin-barman-cloud ObjectStore custom resource
// (barmancloud.cnpg.io/v1) pointing at the shared e2e object store. It is built
// as an unstructured object because the CRD lives in the external plugin and is
// not registered in the operator's scheme.
// Optional mutators are applied to the resulting object right before it is returned,
// allowing callers to customize the spec.
func newObjectStore(namespace, name string, mutators ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	objectStore := &unstructured.Unstructured{}
	objectStore.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "barmancloud.cnpg.io",
		Version: "v1",
		Kind:    "ObjectStore",
	})
	objectStore.SetName(name)
	objectStore.SetNamespace(namespace)
	objectStore.Object["spec"] = map[string]interface{}{
		"configuration": map[string]interface{}{
			"destinationPath": "s3://" + name + "/",
			"endpointURL":     "https://" + objectStoreEnv.ServiceName + ":9000",
			"endpointCA": map[string]interface{}{
				"name": objectStoreEnv.CaSecretName,
				"key":  "ca.crt",
			},
			"s3Credentials": map[string]interface{}{
				"accessKeyId": map[string]interface{}{
					"name": barmanCloudCredentialSecret,
					"key":  "ID",
				},
				"secretAccessKey": map[string]interface{}{
					"name": barmanCloudCredentialSecret,
					"key":  "KEY",
				},
			},
			"wal": map[string]interface{}{
				"compression": "gzip",
			},
		},
	}

	for _, mutate := range mutators {
		mutate(objectStore)
	}

	return objectStore
}
