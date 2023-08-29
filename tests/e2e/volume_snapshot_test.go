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

package e2e

import (
	"encoding/json"
	"os"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test case for validating volume snapshots
// with different storage providers in different k8s environments
var _ = Describe("Verify Volume Snapshot",
	Label(tests.LabelBackupRestore, tests.LabelStorage, tests.LabelSnapshot), func() {
		// Initializing a global namespace variable to be used in each test case
		var namespace string

		Context("Declarative Volume Snapshot", Ordered, func() {
			// test env constants
			const (
				namespacePrefix = "declarative-snapshot-backup"
				level           = tests.High
				filesDir        = fixturesDir + "/volume_snapshot"
				snapshotDataEnv = "SNAPSHOT_NAME_PGDATA"
				snapshotWalEnv  = "SNAPSHOT_NAME_PGWAL"
			)
			// file constants
			const (
				clusterToBackupFilePath  = filesDir + "/declarative-backup-cluster.yaml.template"
				clusterToRestoreFilePath = filesDir + "/declarative-backup-cluster-restore.yaml.template"
				backupFileFilePath       = filesDir + "/declarative-backup.yaml.template"
			)

			// database constants
			const (
				tableName = "test"
			)

			BeforeAll(func() {
				if testLevelEnv.Depth < int(level) {
					Skip("Test depth is lower than the amount requested for this test")
				}

				if !(IsLocal() || IsGKE()) {
					Skip("This test is only executed on gke, openshift and local")
				}

				var err error
				namespace, err = env.CreateUniqueNamespace(namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() error {
					if CurrentSpecReport().Failed() {
						env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
					}
					if err := os.Unsetenv(snapshotDataEnv); err != nil {
						return err
					}
					if err := os.Unsetenv(snapshotWalEnv); err != nil {
						return err
					}
					return env.DeleteNamespace(namespace)
				})
			})

			It("creating a declarative cold backup and restoring it", func() {
				clusterToBackupName, err := env.GetResourceNameFromYAML(clusterToBackupFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("creating the cluster on which to execute the backup", func() {
					AssertCreateCluster(namespace, clusterToBackupName, clusterToBackupFilePath, env)
				})

				By("inserting test data", func() {
					AssertCreateTestData(namespace, clusterToBackupName, tableName, psqlClientPod)
				})

				backupName, err := env.GetResourceNameFromYAML(backupFileFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the backup", func() {
					err := CreateResourcesFromFileWithError(namespace, backupFileFilePath)
					Expect(err).ToNot(HaveOccurred())
				})

				var backup apiv1.Backup
				By("waiting the backup to complete", func() {
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, &backup)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseCompleted))
					}, 300).Should(Succeed())
					AssertBackupConditionInClusterStatus(namespace, clusterToBackupName)
				})

				var clusterToBackup *apiv1.Cluster

				By("fetching the created cluster", func() {
					clusterToBackup, err = env.GetCluster(namespace, clusterToBackupName)
					Expect(err).ToNot(HaveOccurred())
				})

				snapshotList := volumesnapshot.VolumeSnapshotList{}
				By("fetching the volume snapshots", func() {
					err := env.Client.List(env.Ctx, &snapshotList, k8client.MatchingLabels{
						utils.ClusterLabelName: clusterToBackupName,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(snapshotList.Items).To(HaveLen(len(backup.Status.BackupSnapshotStatus.Snapshots)))
				})

				By("ensuring that the additional labels and annotations are present", func() {
					clusterObj := &apiv1.Cluster{}
					for _, item := range snapshotList.Items {
						snapshotConfig := clusterToBackup.Spec.Backup.VolumeSnapshot
						Expect(utils.IsMapSubset(item.Annotations, snapshotConfig.Annotations)).To(BeTrue())
						Expect(utils.IsMapSubset(item.Labels, snapshotConfig.Labels)).To(BeTrue())
						Expect(item.Labels[utils.BackupNameLabelName]).To(BeEquivalentTo(backup.Name))
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.ClusterManifestAnnotationName]).To(ContainSubstring(clusterToBackupName))
						Expect(item.Annotations[utils.PgControldataAnnotationName]).ToNot(BeEmpty())
						Expect(item.Annotations[utils.PgControldataAnnotationName]).To(ContainSubstring("pg_control version number:"))
						// Ensure the ClusterManifestAnnotationName is a valid Cluster Object
						err = json.Unmarshal([]byte(item.Annotations[utils.ClusterManifestAnnotationName]), clusterObj)
						Expect(err).ToNot(HaveOccurred())
					}
				})

				err = testUtils.SetSnapshotNameAsEnv(&snapshotList, snapshotDataEnv, snapshotWalEnv)
				Expect(err).ToNot(HaveOccurred())

				clusterToRestoreName, err := env.GetResourceNameFromYAML(clusterToRestoreFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("executing the restore", func() {
					AssertCreateCluster(namespace, clusterToRestoreName, clusterToRestoreFilePath, env)
				})

				By("checking that the data is present on the restored cluster", func() {
					AssertDataExpectedCount(namespace, clusterToRestoreName, tableName, 2, psqlClientPod)
				})
			})
		})
	})
