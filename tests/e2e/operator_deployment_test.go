/*
Copyright 2019-2022 The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
)

var _ = Describe("PostgreSQL operator deployment", func() {
	const level = tests.Highest

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("sets up the operator", func() {
		By("having a pod for the operator in state ready", func() {
			AssertOperatorIsReady()
		})
		By("having a deployment for the operator in state ready", func() {
			deployment, err := env.GetOperatorDeployment()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).Should(BeEquivalentTo(1))
		})
	})
})
