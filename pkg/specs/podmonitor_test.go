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

package specs

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodMonitor test", func() {
	It("should create a valid monitoringv1.PodMonitor object", func() {
		const (
			clusterName      = "test"
			clusterNamespace = "test-namespace"
		)
		cluster := v1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
		}
		mgr := NewClusterPodMonitorManager(&cluster)
		monitor := mgr.BuildPodMonitor()
		Expect(monitor.Labels[utils.ClusterLabelName]).To(Equal(clusterName))
		Expect(monitor.Spec.Selector.MatchLabels[utils.ClusterLabelName]).To(Equal(clusterName))
		Expect(monitor.Spec.PodMetricsEndpoints).To(ContainElement(monitoringv1.PodMetricsEndpoint{Port: "metrics"}))
	})
})
