/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	pkgutils "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests that set up a cluster with apparmor support enabled
var _ = Describe("AppArmor support", Serial, Label(tests.LabelNoOpenshift), func() {
	const (
		clusterName         = "cluster-apparmor"
		clusterAppArmorFile = fixturesDir + "/apparmor/cluster-apparmor.yaml"
		namespace           = "cluster-apparmor-e2e"
		level               = tests.Low
	)
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		isAKS, err := env.IsAKS()
		Expect(err).ToNot(HaveOccurred())
		if !isAKS {
			Skip("This test case can only run on Azure")
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

	It("sets up a cluster enabling AppArmor annotation feature", func() {
		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, clusterAppArmorFile, env)

		By("verifying AppArmor annotations on cluster and pods", func() {
			// Gathers the pod list using annotations
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				annotation := pod.ObjectMeta.Annotations[pkgutils.AppArmorAnnotationPrefix+"/"+specs.PostgresContainerName]
				Expect(annotation).ShouldNot(BeEmpty(),
					fmt.Sprintf("annotation for apparmor is not on pod %v", specs.PostgresContainerName))
				Expect(annotation).Should(BeEquivalentTo("runtime/default"),
					fmt.Sprintf("annotation value is not set on pod %v", specs.PostgresContainerName))
			}
		})
	})
})
