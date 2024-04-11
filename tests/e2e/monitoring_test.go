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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// Set of tests that set up a cluster with monitoring support enabled
var _ = Describe("PodMonitor support", Serial, Label(tests.LabelPrometheus), func() {
	const (
		namespacePrefix              = "cluster-monitoring-e2e"
		level                        = tests.Medium
		clusterDefaultName           = "cluster-default-monitoring"
		clusterDefaultMonitoringFile = fixturesDir + "/monitoring/cluster-default-monitoring.yaml"
		clusterName                  = "cluster-monitoring"
		poolerName                   = "cluster-pooler-monitoring"
		clusterMonitoringFile        = fixturesDir + "/monitoring/cluster-monitoring.yaml"
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

			podMonitor, err := env.GetPodMonitor(map[string]string{
				utils.ClusterLabelName: clusterDefaultName,
			})
			Expect(err).ToNot(HaveOccurred())

			endpoints := podMonitor.Spec.PodMetricsEndpoints
			Expect(endpoints).Should(HaveLen(1), "endpoints should be of size 1")
			Expect(endpoints[0].Interval).Should(BeEmpty(), "should not be set as spec")
			Expect(endpoints[0].ScrapeTimeout).Should(BeEmpty(), "should not be set as spec")
		})
	})

	It("sets up a cluster enabling PodMonitor feature and custom specs", func() {
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterName, clusterMonitoringFile, env)

		By("verifying PodMonitor properties", func() {
			var podMonitor *monitoringv1.PodMonitor

			podMonitor, err := env.GetPodMonitor(map[string]string{
				utils.ClusterLabelName: clusterName,
			})
			Expect(err).ToNot(HaveOccurred())

			endpoints := podMonitor.Spec.PodMetricsEndpoints
			Expect(endpoints).Should(HaveLen(1), "endpoints should be of size 1")
			Expect(endpoints[0].Interval).Should(Equal(monitoringv1.Duration("30s")), "should be set as spec")
			Expect(endpoints[0].ScrapeTimeout).Should(Equal(monitoringv1.Duration("60s")), "should be set as spec")
		})

		By("verifying Pooler PodMonitor properties", func() {
			var podMonitor *monitoringv1.PodMonitor

			podMonitor, err := env.GetPodMonitor(map[string]string{
				utils.PgbouncerNameLabel: poolerName,
			})
			Expect(err).ToNot(HaveOccurred())

			endpoints := podMonitor.Spec.PodMetricsEndpoints
			Expect(endpoints).Should(HaveLen(1), "endpoints should be of size 1")
			Expect(endpoints[0].Interval).Should(Equal(monitoringv1.Duration("30s")), "should be set as spec")
			Expect(endpoints[0].ScrapeTimeout).Should(Equal(monitoringv1.Duration("60s")), "should be set as spec")
		})
	})
})
