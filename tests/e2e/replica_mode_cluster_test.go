/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

var _ = Describe("Replica Mode", func() {
	const (
		replicaModeClusterDir = "/replica_mode_cluster/"
		srcClusterName        = "cluster-replica-src"
		srcClusterSample      = fixturesDir + replicaModeClusterDir + srcClusterName + ".yaml"
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
	replicaCommandTimeout := time.Second * 2

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(replicaNamespace, srcClusterName,
				"out/"+CurrentSpecReport().LeafNodeText+"-source-cluster.log")
			env.DumpClusterEnv(replicaNamespace, replicaClusterName,
				"out/"+CurrentSpecReport().LeafNodeText+"-replica-cluster.log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(replicaNamespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("can bootstrap a replica cluster using TLS auth", func() {
		const replicaClusterSampleTLS = fixturesDir + replicaModeClusterDir + "cluster-replica-tls.yaml"

		It("should work", func() {
			replicaNamespace = "replica-mode-tls-auth"
			replicaClusterName = "cluster-replica-tls"
			err := env.CreateNamespace(replicaNamespace)
			Expect(err).ToNot(HaveOccurred())
			AssertReplicaModeCluster(replicaNamespace, srcClusterName, srcClusterSample, replicaClusterName,
				replicaClusterSampleTLS, checkQuery)
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		const replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml"

		var primaryReplicaCluster *corev1.Pod
		var err error

		It("still works detached from remote server promoting the designated primary", func() {
			replicaNamespace = "replica-mode-basic-auth"
			replicaClusterName = "cluster-replica-basicauth"

			err = env.CreateNamespace(replicaNamespace)
			Expect(err).ToNot(HaveOccurred())

			AssertReplicaModeCluster(replicaNamespace, srcClusterName, srcClusterSample,
				replicaClusterName, replicaClusterSampleBasicAuth, checkQuery)

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
				query := "CREATE TABLE replica_cluster_primary AS VALUES (1), (2);"
				// Expect write operation to succeed
				Eventually(func() error {
					// Get primary from replica cluster
					primaryReplicaCluster, err = env.GetClusterPrimary(replicaNamespace, replicaClusterName)
					if err != nil {
						return err
					}
					_, _, err = env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
						&replicaCommandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
					return err
				}, 300, 15).ShouldNot(HaveOccurred())
			})

			By("writing some new data to the source cluster", func() {
				insertRecordIntoTable(replicaNamespace, srcClusterName, "test_replica", 4)
			})

			By("verifying that replica cluster was not modified", func() {
				AssertDataExpectedCount(replicaNamespace, primaryReplicaCluster.Name, "test_replica", 3)
			})
		})
	})

	Context("archive mode set to 'always' on designated primary", func() {
		It("verify replica cluster can archive WALs from the designated primary", func() {
			const replicaClusterSample = fixturesDir + replicaModeClusterDir + "cluster-replica-archive-mode-always.yaml"

			replicaNamespace = "replica-mode-archive"
			err := env.CreateNamespace(replicaNamespace)
			Expect(err).ToNot(HaveOccurred())

			replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSample)
			Expect(err).ToNot(HaveOccurred())
			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(replicaNamespace, "backup-storage-creds", "minio", "minio123")
			})
			By("setting up minio", func() {
				minio, err := utils.MinioDefaultSetup(replicaNamespace)
				Expect(err).ToNot(HaveOccurred())
				err = utils.InstallMinio(env, minio, 300)
				Expect(err).ToNot(HaveOccurred())
			})
			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				minioClient := utils.MinioDefaultClient(replicaNamespace)
				err := utils.PodCreateAndWaitForReady(env, &minioClient, 240)
				Expect(err).ToNot(HaveOccurred())
			})

			AssertReplicaModeCluster(replicaNamespace, srcClusterName, srcClusterSample, replicaClusterName,
				replicaClusterSample, checkQuery)

			// Get primary from replica cluster
			primaryReplicaCluster, err := env.GetClusterPrimary(replicaNamespace, replicaClusterName)
			Expect(err).ToNot(HaveOccurred())

			commandTimeout := time.Second * 5

			By("verify archive mode is set to 'always on' designated primary", func() {
				query := "show archive_mode;"
				Eventually(func() (string, error) {
					stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
						&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
					return strings.Trim(stdOut, "\n"), err
				}, 30).Should(BeEquivalentTo("always"))
			})
			By("verify the WALs are archived from the designated primary", func() {
				AssertArchiveWalOnMinio(replicaNamespace, srcClusterName)
			})
		})
	})
})
