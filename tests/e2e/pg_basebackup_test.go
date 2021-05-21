/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strings"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap with pg_basebackup using basic auth", func() {
	const namespace = "cluster-pg-basebackup-basic-auth"

	const srcCluster = fixturesDir + "/pg_basebackup/cluster-src.yaml"
	const srcClusterName = "pg-basebackup-src"

	const dstCluster = fixturesDir + "/pg_basebackup/cluster-dst-basic-auth.yaml"
	const dstClusterName = "pg-basebackup-dst-basic-auth"

	const checkQuery = "psql -U postgres app -tAc 'SELECT count(*) FROM to_bootstrap'"

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, srcClusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+"-src.log")
			env.DumpClusterEnv(namespace, dstClusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+"-dst.log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can bootstrap with pg_basebackup using basic auth", func() {
		primarySrc := setupPgBasebackup(namespace, srcClusterName, srcCluster)

		primaryDst := dstClusterName + "-1"

		By("creating the dst cluster", func() {
			AssertCreateCluster(namespace, dstClusterName, dstCluster, env)

			// We give more time than the usual 600s, since the recovery is slower
			AssertClusterIsReady(namespace, dstClusterName, 800, env)
		})

		By("checking data have been copied correctly", func() {
			// Test data should be present on restored primary
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		By("writing some new data to the dst cluster", func() {
			cmd := "psql -U postgres app -tAc 'INSERT INTO to_bootstrap VALUES (3);'"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				cmd))
			Expect(err).ToNot(HaveOccurred())
		})

		By("checking the src cluster was not modified", func() {
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primarySrc,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Bootstrap with pg_basebackup using TLS auth", func() {
	const namespace = "cluster-pg-basebackup-tls-auth"

	const srcCluster = fixturesDir + "/pg_basebackup/cluster-src.yaml"
	const srcClusterName = "pg-basebackup-src"

	const dstCluster = fixturesDir + "/pg_basebackup/cluster-dst-tls.yaml"
	const dstClusterName = "pg-basebackup-dst-tls-auth"

	const checkQuery = "psql -U postgres app -tAc 'SELECT count(*) FROM to_bootstrap'"

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, srcClusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+"-src.log")
			env.DumpClusterEnv(namespace, dstClusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+"-dst.log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can bootstrap with pg_basebackup using TLS auth", func() {
		primarySrc := setupPgBasebackup(namespace, srcClusterName, srcCluster)

		primaryDst := dstClusterName + "-1"
		By("creating the dst cluster", func() {
			AssertCreateCluster(namespace, dstClusterName, dstCluster, env)

			// We give more time than the usual 600s, since the recovery is slower
			AssertClusterIsReady(namespace, dstClusterName, 800, env)
		})

		By("checking data have been copied correctly", func() {
			// Test data should be present on restored primary
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				checkQuery))
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		By("writing some new data to the dst cluster", func() {
			cmd := "psql -U postgres app -tAc 'INSERT INTO to_bootstrap VALUES (3);'"
			_, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				cmd))
			Expect(err).ToNot(HaveOccurred())
		})

		By("checking the src cluster was not modified", func() {
			out, _, err := tests.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primarySrc,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func setupPgBasebackup(namespace, srcClusterName, srcCluster string) string {
	primarySrc := srcClusterName + "-1"
	// Create a cluster in a namespace we'll delete after the test
	err := env.CreateNamespace(namespace)
	Expect(err).ToNot(HaveOccurred())

	// Create the src Cluster
	AssertCreateCluster(namespace, srcClusterName, srcCluster, env)

	cmd := "psql -U postgres app -tAc 'CREATE TABLE to_bootstrap AS VALUES (1), (2);'"
	_, _, err = tests.Run(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		primarySrc,
		cmd))
	Expect(err).ToNot(HaveOccurred())
	return primarySrc
}
