/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL operator deployment", func() {
	It("sets up the operator", func() {
		By("having a pod for the operator in state ready", func() {
			timeout := 120
			Eventually(func() (bool, error) {
				operatorPod, err := env.GetOperatorPod()
				return utils.IsPodReady(operatorPod), err
			}, timeout).Should(BeTrue())
		})
		By("having a deployment for the operator in state ready", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).Should(BeEquivalentTo(1))
		})
	})
})
