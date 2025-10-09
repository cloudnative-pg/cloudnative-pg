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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PoolerPodMonitorManager", func() {
	var pooler *apiv1.Pooler

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
				//nolint:staticcheck // Using deprecated type during deprecation period
				Monitoring: &apiv1.PoolerMonitoringConfiguration{
					EnablePodMonitor: false,
				},
			},
		}
	})

	Context("when calling IsPodMonitorEnabled", func() {
		It("returns the correct value", func() {
			manager := NewPoolerPodMonitorManager(pooler)

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
			manager := NewPoolerPodMonitorManager(pooler)

			podMonitor := manager.BuildPodMonitor()

			Expect(podMonitor.Namespace).To(Equal(pooler.Namespace))
			Expect(podMonitor.Name).To(Equal(pooler.Name))
			Expect(podMonitor.Labels).To(Equal(map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
			}))

			Expect(podMonitor.Spec.Selector.MatchLabels).To(Equal(map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
				utils.PodRoleLabelName:   string(utils.PodRolePooler),
			}))

			Expect(podMonitor.Spec.PodMetricsEndpoints).To(HaveLen(1))
			Expect(*podMonitor.Spec.PodMetricsEndpoints[0].Port).To(Equal("metrics"))
		})
	})

	Context("when monitoring if not configured", func() {
		It("does not panic", func() {
			pooler := apiv1.Pooler{}
			manager := NewPoolerPodMonitorManager(&pooler)
			podMonitor := manager.BuildPodMonitor()
			Expect(podMonitor).ToNot(BeNil())
		})
	})
})
