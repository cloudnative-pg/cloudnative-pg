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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replica Mode", Label(tests.LabelReplication), func() {
	const (
		replicaModeClusterDir = "/replica_mode_cluster/"
		srcClusterName        = "cluster-replica-src"
		srcClusterSample      = fixturesDir + replicaModeClusterDir + srcClusterName + ".yaml.template"
		checkQuery            = "SELECT count(*) FROM test_replica"
		level                 = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if env.IsIBM() {
			Skip("This test is not run on an IBM architecture")
		}
	})

	// Setting variables
	var replicaClusterName, replicaNamespace string
	replicaCommandTimeout := time.Second * 10

	Context("can bootstrap a replica cluster using TLS auth", func() {
		const replicaClusterSampleTLS = fixturesDir + replicaModeClusterDir + "cluster-replica-tls.yaml.template"

		It("should work", func() {
			replicaNamespacePrefix := "replica-mode-tls-auth"
			replicaClusterName = "cluster-replica-tls"
			var err error
			replicaNamespace, err = env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				srcClusterSample,
				replicaClusterName,
				replicaClusterSampleTLS,
				checkQuery,
				psqlClientPod)
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		const replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml.template"

		var primaryReplicaCluster *corev1.Pod
		var err error

		It("still works detached from remote server promoting the designated primary", func() {
			replicaNamespacePrefix := "replica-mode-basic-auth"
			replicaClusterName = "cluster-replica-basicauth"
			replicaNamespace, err = env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				srcClusterSample,
				replicaClusterName,
				replicaClusterSampleBasicAuth,
				checkQuery,
				psqlClientPod)

			By("disabling the replica mode", func() {
				Eventually(func() error {
					_, _, err = utils.RunUnchecked(fmt.Sprintf(
						"kubectl patch cluster %v -n %v  -p '{\"spec\":{\"replica\":{\"enabled\":false}}}'"+
							" --type='merge'",
						replicaClusterName, replicaNamespace))
					if err != nil {
						return err
					}
					return nil
				}, 60, 5).Should(BeNil())
			})

			By("verifying write operation on the replica cluster primary pod", func() {
				query := "CREATE TABLE IF NOT EXISTS replica_cluster_primary AS VALUES (1),(2);"
				// Expect write operation to succeed
				Eventually(func() error {
					// Get primary from replica cluster
					primaryReplicaCluster, err = env.GetClusterPrimary(replicaNamespace, replicaClusterName)
					if err != nil {
						return err
					}
					_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
						&replicaCommandTimeout, "psql", "-U", "postgres", "appSrc", "-tAc", query)
					return err
				}, 300, 15).ShouldNot(HaveOccurred())
			})

			By("verifying the appTgt database not exist in replica cluster", func() {
				AssertDatabaseExists(replicaNamespace, primaryReplicaCluster.Name, "appTgt", false)
			})

			By("writing some new data to the source cluster", func() {
				insertRecordIntoTableWithDatabaseName(replicaNamespace, srcClusterName, "appSrc", "test_replica", 4, psqlClientPod)
			})

			By("verifying that replica cluster was not modified", func() {
				AssertDataExpectedCountWithDatabaseName(replicaNamespace, primaryReplicaCluster.Name, "appSrc", "test_replica", 3)
			})
		})
	})

	Context("archive mode set to 'always' on designated primary", func() {
		It("verify replica cluster can archive WALs from the designated primary", func() {
			const replicaClusterSample = fixturesDir + replicaModeClusterDir +
				"cluster-replica-archive-mode-always.yaml.template"

			replicaNamespacePrefix := "replica-mode-archive"
			var err error
			replicaNamespace, err = env.CreateUniqueNamespace(replicaNamespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(replicaNamespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(replicaNamespace)
			})
			replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSample)
			Expect(err).ToNot(HaveOccurred())
			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(replicaNamespace, "backup-storage-creds", "minio", "minio123")
			})
			By("setting up minio", func() {
				minio, err := utils.MinioDefaultSetup(replicaNamespace)
				Expect(err).ToNot(HaveOccurred())
				err = utils.InstallMinio(env, minio, uint(testTimeouts[utils.MinioInstallation]))
				Expect(err).ToNot(HaveOccurred())
			})
			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				minioClient := utils.MinioDefaultClient(replicaNamespace)
				err := utils.PodCreateAndWaitForReady(env, &minioClient, 240)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertReplicaModeCluster(
				replicaNamespace,
				srcClusterName,
				srcClusterSample,
				replicaClusterName,
				replicaClusterSample,
				checkQuery,
				psqlClientPod)

			// Get primary from replica cluster
			primaryReplicaCluster, err := env.GetClusterPrimary(replicaNamespace, replicaClusterName)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 10

			By("verify archive mode is set to 'always on' designated primary", func() {
				query := "show archive_mode;"
				Eventually(func() (string, error) {
					stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
						&commandTimeout, "psql", "-U", "postgres", "appSrc", "-tAc", query)
					return strings.Trim(stdOut, "\n"), err
				}, 30).Should(BeEquivalentTo("always"))
			})
			By("verify the WALs are archived from the designated primary", func() {
				// only replica cluster has backup configure to minio,
				// need the server name  be replica cluster name here
				AssertArchiveWalOnMinio(replicaNamespace, srcClusterName, replicaClusterName)
			})
		})
	})
})
