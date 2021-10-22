/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster with some post-init-sql query and verifying that they are really executed

// Set of tests in which we check that the initdb options are really applied
var _ = Describe("InitDB settings", func() {
	const (
		fixturesCertificatesDir = fixturesDir + "/initdb"
		level                   = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("initdb custom post-init SQL scripts", func() {
		const (
			clusterName        = "p-postinit-sql"
			postInitSQLCluster = fixturesCertificatesDir + "/cluster-postinit-sql.yaml"
		)

		var namespace string
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can find the tables created by the post-init SQL queries", func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace = "initdb-postqueries"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			primaryDst := clusterName + "-1"

			By("querying the tables via psql", func() {
				cmd := "psql -U postgres postgres -tAc 'SELECT count(*) FROM numbers'"
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryDst,
					cmd))
				Expect(err).ToNot(HaveOccurred())
			})
			By("checking inside the database the default locale", func() {
				cmd := "psql -U postgres postgres -tAc \"select datcollate from pg_database where datname='template0'\""
				stdout, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryDst,
					cmd))
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("C\n"))
			})
		})
	})

	Context("custom default locale", func() {
		const (
			clusterName        = "p-locale"
			postInitSQLCluster = fixturesCertificatesDir + "/cluster-custom-locale.yaml"
		)

		var namespace string
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpClusterEnv(namespace, clusterName,
					"out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		AfterEach(func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		It("use the custom default locale specified", func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace = "initdb-locale"
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			primaryDst := clusterName + "-1"

			By("checking inside the database", func() {
				cmd := "psql -U postgres postgres -tAc \"select datcollate from pg_database where datname='template0'\""
				stdout, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryDst,
					cmd))
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("en_US.utf8\n"))
			})
		})
	})
})
