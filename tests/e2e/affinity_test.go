/*
Copyright The CloudNativePG Contributors

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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("E2E Affinity", Serial, Label(tests.LabelPodScheduling), func() {
	const (
		clusterFile     = fixturesDir + "/affinity/cluster-affinity.yaml"
		poolerFile      = fixturesDir + "/affinity/pooler-affinity.yaml"
		clusterName     = "cluster-affinity"
		namespacePrefix = "test-affinity"
		level           = tests.Medium
	)
	var namespace string
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can create a cluster and a pooler with required affinity", func() {
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, clusterFile, env)
		createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerFile, 3)

		_, _, err := run.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
		Expect(err).ToNot(HaveOccurred())
		AssertClusterIsReady(namespace, clusterName, 300, env)
	})
})
