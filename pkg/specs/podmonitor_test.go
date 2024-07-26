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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodMonitor test", func() {
	const (
		clusterName      = "test"
		clusterNamespace = "test-namespace"
	)

	assertPodMonitorCorrect := func(cluster *apiv1.Cluster, expectedEndpoint monitoringv1.PodMetricsEndpoint) {
		getMetricRelabelings := func() []monitoringv1.RelabelConfig {
			return []monitoringv1.RelabelConfig{
				{
					SourceLabels: []monitoringv1.LabelName{"cluster"},
					TargetLabel:  "cnpg_cluster",
				},
				{
					Regex:  "cluster",
					Action: "labeldrop",
				},
			}
		}
		getRelabelings := func() []monitoringv1.RelabelConfig {
			return []monitoringv1.RelabelConfig{
				{
					SourceLabels: []monitoringv1.LabelName{"__their_label"},
					TargetLabel:  "my_label",
				},
			}
		}

		It("should create a valid monitoringv1.PodMonitor object", func() {
			mgr := NewClusterPodMonitorManager(cluster.DeepCopy())
			monitor := mgr.BuildPodMonitor()
			Expect(monitor.Labels[utils.ClusterLabelName]).To(Equal(cluster.Name))
			Expect(monitor.Spec.Selector.MatchLabels[utils.ClusterLabelName]).To(Equal(cluster.Name))
			Expect(monitor.Spec.PodMetricsEndpoints).To(ContainElement(expectedEndpoint))
		})

		It("should create a monitoringv1.PodMonitor object with MetricRelabelConfigs rules", func() {
			relabeledCluster := cluster.DeepCopy()
			relabeledCluster.Spec.Monitoring.PodMonitorMetricRelabelConfigs = getMetricRelabelings()
			mgr := NewClusterPodMonitorManager(relabeledCluster)
			monitor := mgr.BuildPodMonitor()

			expectedEndpoint := expectedEndpoint.DeepCopy()
			expectedEndpoint.MetricRelabelConfigs = getMetricRelabelings()
			Expect(monitor.Spec.PodMetricsEndpoints).To(ContainElement(*expectedEndpoint))
		})

		It("should create a monitoringv1.PodMonitor object with RelabelConfigs rules", func() {
			relabeledCluster := cluster.DeepCopy()
			relabeledCluster.Spec.Monitoring.PodMonitorRelabelConfigs = getRelabelings()
			mgr := NewClusterPodMonitorManager(relabeledCluster)
			monitor := mgr.BuildPodMonitor()

			expectedEndpoint := expectedEndpoint.DeepCopy()
			expectedEndpoint.RelabelConfigs = getRelabelings()
			Expect(monitor.Spec.PodMetricsEndpoints).To(ContainElement(*expectedEndpoint))
		})

		It("should create a monitoringv1.PodMonitor object with MetricRelabelConfigs and RelabelConfigs rules", func() {
			relabeledCluster := cluster.DeepCopy()
			relabeledCluster.Spec.Monitoring.PodMonitorMetricRelabelConfigs = getMetricRelabelings()
			relabeledCluster.Spec.Monitoring.PodMonitorRelabelConfigs = getRelabelings()
			mgr := NewClusterPodMonitorManager(relabeledCluster)
			monitor := mgr.BuildPodMonitor()

			expectedEndpoint := expectedEndpoint.DeepCopy()
			expectedEndpoint.MetricRelabelConfigs = getMetricRelabelings()
			expectedEndpoint.RelabelConfigs = getRelabelings()
			Expect(monitor.Spec.PodMetricsEndpoints).To(ContainElement(*expectedEndpoint))
		})

		It("does not panic if monitoring section is not present", func() {
			cluster := apiv1.Cluster{}
			mgr := NewClusterPodMonitorManager(&cluster)
			Expect(mgr.BuildPodMonitor()).ToNot(BeNil())
		})
	}

	When("not using TLS", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			Spec: apiv1.ClusterSpec{
				Monitoring: &apiv1.MonitoringConfiguration{
					EnablePodMonitor: true,
				},
			},
		}

		expectedEndpoint := monitoringv1.PodMetricsEndpoint{Port: "metrics"}

		assertPodMonitorCorrect(&cluster, expectedEndpoint)
	})
	When("TLS is enabled for metrics", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			Spec: apiv1.ClusterSpec{
				Monitoring: &apiv1.MonitoringConfiguration{
					EnablePodMonitor: true,
					TLSConfig: &apiv1.ClusterMonitoringTLSConfiguration{
						Enabled: true,
					},
				},
			},
		}

		expectedEndpoint := monitoringv1.PodMetricsEndpoint{
			Port:   "metrics",
			Scheme: "https",
			TLSConfig: &monitoringv1.SafeTLSConfig{
				CA: monitoringv1.SecretOrConfigMap{
					Secret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-ca",
						},
						Key: certs.CACertKey,
					},
				},
				Cert:               monitoringv1.SecretOrConfigMap{},
				ServerName:         ptr.To(cluster.GetServiceReadWriteName()),
				InsecureSkipVerify: ptr.To(true),
			},
		}

		assertPodMonitorCorrect(&cluster, expectedEndpoint)
	})
})
