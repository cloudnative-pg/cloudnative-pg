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

package install

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("generateExecutor", func() {
	var (
		cmd *generateExecutor
		dep *appsv1.Deployment
	)

	BeforeEach(func() {
		cmd = &generateExecutor{
			logFieldLevel:     "info",
			logFieldTimestamp: "timestamp",
			replicas:          3,
			nodeSelector:      []string{"key1=value1", "key2=value2"},
		}
		dep = &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			},
		}
	})

	Describe("reconcileOperatorDeployment", func() {
		Context("with valid node selector", func() {
			It("should set the correct values", func() {
				err := cmd.reconcileOperatorDeployment(dep)
				Expect(err).NotTo(HaveOccurred())
				Expect(*dep.Spec.Replicas).To(Equal(int32(3)))
				depSpec := dep.Spec.Template.Spec
				Expect(depSpec.Containers[0].Args).To(ContainElement("--log-field-level=info"))
				Expect(depSpec.Containers[0].Args).To(ContainElement("--log-field-timestamp=timestamp"))
				Expect(depSpec.Affinity).NotTo(BeNil())
				Expect(depSpec.Affinity.NodeAffinity).NotTo(BeNil())
				Expect(depSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
				Expect(depSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).
					To(HaveLen(2))
			})
		})

		Context("with invalid node selector", func() {
			BeforeEach(func() {
				cmd.nodeSelector = []string{"invalid-selector"}
			})

			It("should return an error", func() {
				err := cmd.reconcileOperatorDeployment(dep)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError("invalid node-selector value: invalid-selector, " +
					"must be in the format <labelName>=<labelValue>"))
			})
		})

		Context("with no node selector", func() {
			BeforeEach(func() {
				cmd.nodeSelector = []string{}
			})

			It("should not set affinity", func() {
				err := cmd.reconcileOperatorDeployment(dep)
				Expect(err).NotTo(HaveOccurred())
				Expect(*dep.Spec.Replicas).To(Equal(int32(3)))
				depSpec := dep.Spec.Template.Spec
				Expect(depSpec.Containers[0].Args).To(ContainElement("--log-field-level=info"))
				Expect(depSpec.Containers[0].Args).To(ContainElement("--log-field-timestamp=timestamp"))
				Expect(depSpec.Affinity).To(BeNil())
			})
		})

		Context("with zero replicas", func() {
			BeforeEach(func() {
				cmd.replicas = 0
			})

			It("should not override replicas", func() {
				err := cmd.reconcileOperatorDeployment(dep)
				Expect(err).NotTo(HaveOccurred())
				Expect(dep.Spec.Replicas).To(BeNil())
			})
		})
	})

	Describe("reconcileOperatorConfigMap", func() {
		var cm *corev1.ConfigMap

		BeforeEach(func() {
			cm = &corev1.ConfigMap{
				Data: map[string]string{
					"POSTGRES_IMAGE_NAME": "postgres:latest",
				},
			}
		})

		Context("with watchNamespace set", func() {
			BeforeEach(func() {
				cmd.watchNamespace = "test-namespace"
			})

			It("should set WATCH_NAMESPACE in the config map", func() {
				err := cmd.reconcileOperatorConfigMap(cm)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm.Data["WATCH_NAMESPACE"]).To(Equal("test-namespace"))
			})
		})
	})
})
