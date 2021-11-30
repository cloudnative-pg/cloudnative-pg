/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

var _ = Describe("PGBouncer Types", func() {
	const (
		sampleFile                    = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerCertificateRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer_types/pgbouncer-pooler-rw.yaml"
		poolerCertificateROSampleFile = fixturesDir + "/pgbouncer/pgbouncer_types/pgbouncer-pooler-ro.yaml"
		level                         = tests.Low
		poolerResourceNameRW          = "pooler-connection-rw"
		poolerResourceNameRO          = "pooler-connection-ro"
		poolerServiceRW               = "cluster-pgbouncer-rw"
		poolerServiceRO               = "cluster-pgbouncer-ro"
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
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should have proper service ip and host details for ro and rw with default installation", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-types"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 2)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 2)
		})

		By("verify that read-only pooler endpoints contain the correct pod addresses", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateROSampleFile, 2)
		})

		By("verify that read-only pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateROSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRO, podList)
		})

		By("verify that read-write pooler endpoints contain the correct pod addresses.", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateRWSampleFile, 2)
		})

		By("verify that read-write pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateRWSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRW, podList)
		})
	})

	It("should have proper service ip and host details for ro and rw after scale up and scale down", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-types-scale"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			// creating pgbouncer read write pooler
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 2)
		})

		By("setting up read only type pgbouncer pooler", func() {
			// creating pgbouncer read only pooler
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 2)
		})

		By("scaling up PGBouncer to 3 instance", func() {
			// scale up command for 3 replicas for readonly
			command := fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=3",
				poolerResourceNameRO, namespace)
			_, _, err := utils.Run(command)
			Expect(err).ToNot(HaveOccurred())

			// verifying if PGBouncer pooler pods are ready after scale up
			assertPGBouncerPodsAreReady(namespace, poolerCertificateROSampleFile, 3)

			// // scale up command for 3 replicas for read write
			command = fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=3", poolerResourceNameRW, namespace)
			_, _, err = utils.Run(command)
			Expect(err).ToNot(HaveOccurred())

			// verifying if PGBouncer pooler pods are ready after scale up
			assertPGBouncerPodsAreReady(namespace, poolerCertificateRWSampleFile, 3)
		})

		By("SCALE UP: verify that read-only pooler endpoints contain the correct pod addresses", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateROSampleFile, 3)
		})

		By("SCALE UP: verify that read-only pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateROSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRO, podList)
		})

		By("SCALE UP: verify that read-write pooler endpoints contain the correct pod addresses.", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateRWSampleFile, 3)
		})

		By("SCALE UP: verify that read-write pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateRWSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRW, podList)
		})

		By("scaling down PGBouncer to 1 instance", func() {
			// scale down command for 1 replicas for readonly
			command := fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=1",
				poolerResourceNameRO, namespace)
			_, _, err := utils.Run(command)
			Expect(err).ToNot(HaveOccurred())

			// verifying if PGBouncer pooler pods are ready
			assertPGBouncerPodsAreReady(namespace, poolerCertificateROSampleFile, 1)

			// scale down command for 1 replicas for read write
			command = fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=1", poolerResourceNameRW, namespace)
			_, _, err = utils.Run(command)
			Expect(err).ToNot(HaveOccurred())

			// verifying if PGBouncer pooler pods are ready
			assertPGBouncerPodsAreReady(namespace, poolerCertificateRWSampleFile, 1)
		})

		By("SCALE DOWN: verify that read-only pooler endpoints contains the correct pod addresses.", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateROSampleFile, 1)
		})

		By("SCALE DOWN: verify that read-only pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateROSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRO, podList)
		})

		By("SCALE DOWN: verify that read-write pooler endpoints contain the correct pod addresses.", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateRWSampleFile, 1)
		})

		By("SCALE DOWN: verify that read-write pooler pgbouncer.ini contains the correct host service", func() {
			podList, err := utils.GetPGBouncerPodList(namespace, poolerCertificateRWSampleFile, env)
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRW, podList)
		})
	})
})
