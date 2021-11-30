/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
)

var _ = Describe("PGBouncer Connections", func() {
	const (
		sampleFile                    = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		poolerCertificateRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-rw.yaml"
		poolerBasicAuthROSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-ro.yaml"
		poolerCertificateROSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-ro.yaml"
		level                         = tests.Low
	)

	var namespace, clusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
			env.DumpPoolerResourcesInfo(namespace, CurrentSpecReport().LeafNodeText)
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can connect to Postgres via pgbouncer service using basic auth", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-basic-auth"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
		})

		By("verifying read and write connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		By("verifying read connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})

	It("can connect to Postgres via pgbouncer service using tls certificates", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-certificate"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 1)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 1)
		})

		By("verifying read and write connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateRWSampleFile, true)
		})

		By("verifying read connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateROSampleFile, false)
		})
	})

	It("can connect to Postgres via pgbouncer against a cluster with separate client and server CA", func() {
		const (
			folderPath                    = fixturesDir + "/pgbouncer/pgbouncer_separate_client_server_ca/"
			sampleFileWithCertificate     = folderPath + "cluster-user-supplied-client-server-certificates.yaml"
			poolerCertificateROSampleFile = folderPath + "pgbouncer-pooler-tls-ro.yaml"
			poolerCertificateRWSampleFile = folderPath + "pgbouncer-pooler-tls-rw.yaml"
			caSecName                     = "my-postgresql-server-ca"
			tlsSecName                    = "my-postgresql-server"
			tlsSecNameClient              = "my-postgresql-client"
			caSecNameClient               = "my-postgresql-client-ca"
		)
		// Create a cluster in a namespace that will be deleted after the test
		namespace = "pgbouncer-separate-certificates"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFileWithCertificate)
		Expect(err).ToNot(HaveOccurred())

		// Create certificates secret for server
		CreateAndAssertCertificatesSecrets(namespace, clusterName,
			caSecName, tlsSecName, certs.CertTypeServer, true)

		// Create certificates secret for client
		CreateAndAssertCertificatesSecrets(namespace, clusterName,
			caSecNameClient, tlsSecNameClient, certs.CertTypeClient, true)

		AssertCreateCluster(namespace, clusterName, sampleFileWithCertificate, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 1)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 1)
		})

		By("verifying read and write connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateRWSampleFile, true)
		})

		By("verifying read connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateROSampleFile, false)
		})
	})
})

var _ = Describe("PgBouncer Pooler Resources", func() {
	const (
		sampleFile                  = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		poolerBasicAuthROSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-ro.yaml"
		level                       = tests.Low
	)
	var namespace, clusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
			env.DumpPoolerResourcesInfo(namespace, CurrentSpecReport().LeafNodeText)
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should recreate after deleting pgbouncer pod", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-pod-delete"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})
		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
		})

		assertPodIsRecreated(namespace, poolerBasicAuthRWSampleFile)
		By("verifying pgbouncer read write service connections after deleting pod", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		assertPodIsRecreated(namespace, poolerBasicAuthROSampleFile)
		By("verifying pgbouncer read only service connections after pod deleting", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})
	It("should recreate after deleting pgbouncer deployment", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-deployment-delete"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})
		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
		})

		assertDeploymentIsRecreated(namespace, poolerBasicAuthRWSampleFile)
		By("verifying pgbouncer read write service connections after deleting deployment", func() {
			// verify read and write connections after pgbouncer deployment deletion
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		assertDeploymentIsRecreated(namespace, poolerBasicAuthROSampleFile)
		By("verifying pgbouncer read only service connections after deleting deployment", func() {
			// verify read and write connections after pgbouncer deployment deletion
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})
})
