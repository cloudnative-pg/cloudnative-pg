/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package e2e

import (
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL operator deployment", Label(tests.LabelBasic, tests.LabelOperator), func() {
	const level = tests.Highest

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("sets up the operator", func() {
		By("having a pod for the operator in state ready", func() {
			Expect(operator.WaitForReady(env.Ctx, env.Client, uint(testTimeouts[timeouts.OperatorIsReady]),
				true)).Should(Succeed())
		})
		By("having a deployment for the operator in state ready", func() {
			ready, err := operator.IsReady(env.Ctx, env.Client, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(ready).To(BeTrue())
		})
	})
})
