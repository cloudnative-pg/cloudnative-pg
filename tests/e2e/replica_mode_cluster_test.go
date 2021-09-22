/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
)

var _ = Describe("Replica Mode", func() {
	commandTimeout := time.Second * 2

	Context("can bootstrap a replica cluster using TLS auth", func() {
		const (
			replicaModeClusterDir   = "/replica_mode_cluster/"
			srcClusterName          = "cluster-replica-src"
			srcClusterSample        = fixturesDir + replicaModeClusterDir + srcClusterName + ".yaml"
			replicaClusterSampleTLS = fixturesDir + replicaModeClusterDir + "cluster-replica-tls.yaml"
			checkQuery              = "SELECT count(*) FROM test_replica"
		)

		// Setting context variables
		namespace := "replica-mode-tls-auth"
		replicaClusterName := "cluster-replica-tls"

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, srcClusterName,
					"out/"+CurrentSpecReport().LeafNodeText+"-source-cluster.log")
				env.DumpClusterEnv(namespace, replicaClusterName,
					"out/"+CurrentSpecReport().LeafNodeText+"-replica-cluster.log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should work", func() {
			AssertReplicaModeCluster(namespace, srcClusterName, srcClusterSample, replicaClusterName,
				replicaClusterSampleTLS, checkQuery)
		})
	})

	Context("can bootstrap a replica cluster using basic auth", func() {
		const (
			replicaModeClusterDir         = "/replica_mode_cluster/"
			srcClusterName                = "cluster-replica-src"
			srcClusterSample              = fixturesDir + replicaModeClusterDir + srcClusterName + ".yaml"
			replicaClusterSampleBasicAuth = fixturesDir + replicaModeClusterDir + "cluster-replica-basicauth.yaml"
			checkQuery                    = "SELECT count(*) FROM test_replica"
		)

		// Setting context variables
		namespace := "replica-mode-basic-auth"
		replicaClusterName := "cluster-replica-basicauth"

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, srcClusterName,
					"out/"+CurrentSpecReport().LeafNodeText+"-source-cluster.log")
				env.DumpClusterEnv(namespace, replicaClusterName,
					"out/"+CurrentSpecReport().LeafNodeText+"-replica-cluster.log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		var primarySrcCluster, primaryReplicaCluster *corev1.Pod
		var err error

		It("still works detached from remote server promoting the designated primary", func() {
			// Create a cluster first
			By("creating a replica cluster", func() {
				AssertReplicaModeCluster(namespace, srcClusterName, srcClusterSample,
					replicaClusterName, replicaClusterSampleBasicAuth, checkQuery)
				// Get primary from source cluster
				primarySrcCluster, err = env.GetClusterPrimary(namespace, srcClusterName)
				Expect(err).ToNot(HaveOccurred())

				// Get primary from replica cluster
				primaryReplicaCluster, err = env.GetClusterPrimary(namespace, replicaClusterName)
				Expect(err).ToNot(HaveOccurred())
			})

			By("disabling the replica mode", func() {
				_, _, err = tests.Run(fmt.Sprintf(
					"kubectl patch cluster %v -n %v  -p '{\"spec\":{\"replica\":{\"enabled\":false}}}'"+
						" --type='merge'",
					replicaClusterName, namespace))
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying that replica designated primary has become an active primary", func() {
				query := "select pg_is_in_recovery();"
				Eventually(func() (string, error) {
					stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
						&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
					return strings.Trim(stdOut, "\n"), err
				}, 300, 15).Should(BeEquivalentTo("f"))
			})

			By("verifying write operation on the replica cluster primary pod", func() {
				query := "CREATE TABLE replica_cluster_primary AS VALUES (1), (2);"
				// Expect write operation to succeed
				_, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
				Expect(err).ToNot(HaveOccurred())
			})

			By("writing some new data to the source cluster", func() {
				query := "INSERT INTO test_replica VALUES (4);"
				_, _, err := env.ExecCommand(env.Ctx, *primarySrcCluster, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying that replica cluster was not modified", func() {
				stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", checkQuery)
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.Trim(stdOut, "\n")).To(BeEquivalentTo("3"))
			})
		})
	})
})

func AssertReplicaModeCluster(
	namespace,
	srcClusterName,
	srcClusterSample,
	replicaClusterName,
	replicaClusterSample,
	checkQuery string) {
	commandTimeout := time.Second * 2
	var primarySrcCluster, primaryReplicaCluster *corev1.Pod
	var err error

	err = env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	By("creating source cluster", func() {
		// Create replica source cluster
		AssertCreateCluster(namespace, srcClusterName, srcClusterSample, env)
		// Get primary from source cluster
		Eventually(func() error {
			primarySrcCluster, err = env.GetClusterPrimary(namespace, srcClusterName)
			return err
		}, 5).Should(BeNil())
	})

	By("creating test data in source cluster", func() {
		cmd := "CREATE TABLE test_replica AS VALUES (1), (2);"
		_, _, err := env.ExecCommand(env.Ctx, *primarySrcCluster, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", cmd)
		Expect(err).ToNot(HaveOccurred())
	})

	By("creating replica cluster", func() {
		AssertCreateCluster(namespace, replicaClusterName, replicaClusterSample, env)
		// Get primary from replica cluster
		Eventually(func() error {
			primaryReplicaCluster, err = env.GetClusterPrimary(namespace, replicaClusterName)
			return err
		}, 5).Should(BeNil())
	})

	By("verifying that replica cluster primary is in recovery mode", func() {
		query := "select pg_is_in_recovery();"
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
			return strings.Trim(stdOut, "\n"), err
		}, 300, 15).Should(BeEquivalentTo("t"))
	})

	By("checking data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 10).Should(BeEquivalentTo("2"))
	})

	By("writing some new data to the source cluster", func() {
		cmd := "INSERT INTO test_replica VALUES (3);"
		_, _, err := env.ExecCommand(env.Ctx, *primarySrcCluster, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", cmd)
		Expect(err).ToNot(HaveOccurred())
	})

	By("checking new data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 15).Should(BeEquivalentTo("3"))
	})
}
