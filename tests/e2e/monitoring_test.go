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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests that set up a cluster with monitoring support enabled
var _ = Describe("PodMonitor support", Serial, Label(tests.LabelObservability), func() {
	const (
		namespacePrefix              = "cluster-monitoring-e2e"
		level                        = tests.Medium
		clusterDefaultName           = "cluster-default-monitoring"
		clusterDefaultMonitoringFile = fixturesDir + "/monitoring/cluster-default-monitoring.yaml"
	)
	var err error
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// Add schema to client so we can use it
		err := monitoringv1.AddToScheme(env.Scheme)
		if err != nil {
			Fail(fmt.Sprintf("Failed to add monitoring v1 scheme: %v", err))
		}

		// Check if CRD exists, otherwise test is invalid
		exist, _ := utils.PodMonitorExist(env.APIExtensionClient.Discovery())
		if !exist {
			Skip("PodMonitor resource is not available")
		}
	})

	It("sets up a cluster enabling PodMonitor feature", func() {
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterDefaultName, clusterDefaultMonitoringFile, env)

		By("verifying PodMonitor existence", func() {
			var podMonitor *monitoringv1.PodMonitor

			podMonitor, err := env.GetPodMonitor(namespace, clusterDefaultName)
			Expect(err).ToNot(HaveOccurred())

			endpoints := podMonitor.Spec.PodMetricsEndpoints
			Expect(endpoints).Should(HaveLen(1), "endpoints should be of size 1")
			Expect(endpoints[0].Interval).Should(BeEmpty(), "should not be set as spec")
			Expect(endpoints[0].ScrapeTimeout).Should(BeEmpty(), "should not be set as spec")
		})
	})
})
