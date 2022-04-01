/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Switchover", func() {
	const (
		namespace   = "switchover-e2e"
		sampleFile  = samplesDir + "/cluster-storage-class.yaml"
		clusterName = "postgresql-storage-class"
		level       = tests.Medium
	)
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
	It("reacts to switchover requests", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		AssertSwitchover(namespace, clusterName, env)
	})
})
