/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

var _ = Describe("Cluster objectmeta", func() {
	const (
		level                 = tests.Low
		clusterWithObjectMeta = fixturesDir + "/cluster_objectmeta/cluster-level-objectMeta.yaml"
		namespace             = "objectmeta-inheritance"
	)
	var clusterName string

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

	It("verify label's and annotation's inheritance when per-cluster objectmeta changed ", func() {
		clusterName := "objectmeta-inheritance"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterWithObjectMeta, env)

		By("checking the pods have the expected labels", func() {
			expectedLabels := map[string]string{
				"environment":      "qaEnv",
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveLabels(env, namespace, clusterName, expectedLabels)
			}, 180).Should(BeTrue())
		})
		By("checking the pods have the expected annotations", func() {
			expectedAnnotations := map[string]string{
				"categories":       "DatabaseApplication",
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveAnnotations(env, namespace, clusterName, expectedAnnotations)
			}, 180).Should(BeTrue())
		})
	})
})
