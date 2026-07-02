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

package pgbouncer

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PoolerPodMonitorManager", func() {
	var pooler *apiv1.Pooler
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pooler",
				APIVersion: "apiv1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
			Spec: apiv1.PoolerSpec{
				Monitoring: &apiv1.PoolerMonitoringConfiguration{
					EnablePodMonitor: false,
				},
			},
		}

		cluster = &apiv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Cluster",
				APIVersion: "apiv1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}
		cluster.SetDefaults()
	})

	Context("when calling IsPodMonitorEnabled", func() {
		It("returns the correct value", func() {
			manager := NewPoolerPodMonitorManager(pooler, cluster)

			Expect(manager.IsPodMonitorEnabled()).To(BeFalse())

			//nolint:staticcheck
			pooler.Spec.Monitoring.EnablePodMonitor = true
			Expect(manager.IsPodMonitorEnabled()).To(BeTrue())
		})
	})

	Context("when calling BuildPodMonitor", func() {
		BeforeEach(func() {
			//nolint:staticcheck
			pooler.Spec.Monitoring.EnablePodMonitor = true
		})

		It("returns the correct PodMonitor object", func() {
			manager := NewPoolerPodMonitorManager(pooler, cluster)

			podMonitor := manager.BuildPodMonitor()

			Expect(podMonitor.Namespace).To(Equal(pooler.Namespace))
			Expect(podMonitor.Name).To(Equal(pooler.Name))
			Expect(podMonitor.Labels).To(Equal(map[string]string{
				utils.PgbouncerNameLabel:              pooler.Name,
				utils.PodRoleLabelName:                string(utils.PodRolePooler),
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  pooler.Name,
				utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			}))

			Expect(podMonitor.Spec.Selector.MatchLabels).To(Equal(map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
				utils.PodRoleLabelName:   string(utils.PodRolePooler),
			}))

			Expect(podMonitor.Spec.PodMetricsEndpoints).To(HaveLen(1))
			Expect(*podMonitor.Spec.PodMetricsEndpoints[0].Port).To(Equal("metrics"))
		})

		It("does not set TLS config when metrics TLS is disabled", func() {
			manager := NewPoolerPodMonitorManager(pooler, cluster)
			podMonitor := manager.BuildPodMonitor()

			Expect(podMonitor.Spec.PodMetricsEndpoints).To(HaveLen(1))
			endpoint := podMonitor.Spec.PodMetricsEndpoints[0]
			Expect(endpoint.Scheme).To(BeNil())
			Expect(endpoint.TLSConfig).To(BeNil())
		})

		It("sets HTTPS scheme and TLS config when metrics TLS is enabled", func() {
			pooler.Spec.Monitoring.TLSConfig = &apiv1.PoolerMonitoringTLSConfiguration{Enabled: true} //nolint:staticcheck
			pooler.Spec.PgBouncer = &apiv1.PgBouncerSpec{
				ClientCASecret: &apiv1.LocalObjectReference{Name: "my-client-ca"},
			}

			manager := NewPoolerPodMonitorManager(pooler, cluster)
			podMonitor := manager.BuildPodMonitor()

			Expect(podMonitor.Spec.PodMetricsEndpoints).To(HaveLen(1))
			endpoint := podMonitor.Spec.PodMetricsEndpoints[0]
			Expect(endpoint.Scheme).To(HaveValue(Equal(monitoringv1.SchemeHTTPS)))
			Expect(endpoint.TLSConfig).ToNot(BeNil())
			Expect(endpoint.TLSConfig.CA.Secret).ToNot(BeNil())
			Expect(endpoint.TLSConfig.CA.Secret.Name).To(Equal("my-client-ca"))
			Expect(endpoint.TLSConfig.CA.Secret.Key).To(Equal("ca.crt"))
			Expect(endpoint.TLSConfig.ServerName).To(HaveValue(Equal(pooler.Name)))
			Expect(endpoint.TLSConfig.InsecureSkipVerify).To(HaveValue(BeTrue()))
		})

		It("falls back to the cluster client CA secret when no clientCASecret is set", func() {
			pooler.Spec.Monitoring.TLSConfig = &apiv1.PoolerMonitoringTLSConfiguration{Enabled: true} //nolint:staticcheck

			manager := NewPoolerPodMonitorManager(pooler, cluster)
			podMonitor := manager.BuildPodMonitor()

			Expect(podMonitor.Spec.PodMetricsEndpoints).To(HaveLen(1))
			endpoint := podMonitor.Spec.PodMetricsEndpoints[0]
			Expect(endpoint.TLSConfig).ToNot(BeNil())
			Expect(endpoint.TLSConfig.CA.Secret.Name).To(Equal(cluster.GetClientCASecretName()))
		})
	})

	Context("when monitoring if not configured", func() {
		It("does not panic", func() {
			pooler := apiv1.Pooler{}
			manager := NewPoolerPodMonitorManager(&pooler, cluster)
			podMonitor := manager.BuildPodMonitor()
			Expect(podMonitor).ToNot(BeNil())
		})
	})
})
