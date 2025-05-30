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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	pkgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests that set up a cluster with apparmor support enabled
var _ = Describe("AppArmor support", Serial, Label(tests.LabelNoOpenshift, tests.LabelSecurity), func() {
	const (
		clusterName         = "cluster-apparmor"
		clusterAppArmorFile = fixturesDir + "/apparmor/cluster-apparmor.yaml"
		namespacePrefix     = "cluster-apparmor-e2e"
		level               = tests.Low
	)
	var err error
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !MustGetEnvProfile().CanRunAppArmor() {
			Skip("environment does not support AppArmor")
		}
	})

	It("sets up a cluster enabling AppArmor annotation feature", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, clusterAppArmorFile, env)

		By("verifying AppArmor annotations on cluster and pods", func() {
			// Gathers the pod list using annotations
			podList, _ := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			for _, pod := range podList.Items {
				annotation := pod.Annotations[pkgutils.AppArmorAnnotationPrefix+"/"+specs.PostgresContainerName]
				Expect(annotation).ShouldNot(BeEmpty(),
					fmt.Sprintf("annotation for apparmor is not on pod %v", specs.PostgresContainerName))
				Expect(annotation).Should(BeEquivalentTo("runtime/default"),
					fmt.Sprintf("annotation value is not set on pod %v", specs.PostgresContainerName))
			}
		})
	})
})
