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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	config "github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Deployment", func() {
	var (
		pooler  *apiv1.Pooler
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "test-cluster"},
				Type:      apiv1.PoolerTypeRW,
				Instances: ptr.To(int32(1)),
				Template:  &apiv1.PodTemplateSpec{},
				PgBouncer: &apiv1.PgBouncerSpec{
					PoolMode:  apiv1.PgBouncerPoolModeSession,
					AuthQuery: "test",
				},
				DeploymentStrategy: &appsv1.DeploymentStrategy{
					Type: appsv1.RollingUpdateDeploymentStrategyType,
				},
				//nolint:staticcheck // Using deprecated type during deprecation period
				Monitoring: &apiv1.PoolerMonitoringConfiguration{
					EnablePodMonitor: true,
				},
			},
		}

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}
	})

	It("creates a Deployment correctly", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())

		expectedHash, err := computeTemplateHash(pooler, config.Current.OperatorImageName)
		Expect(err).ShouldNot(HaveOccurred())

		// Check the computed hash
		Expect(deployment.ObjectMeta.Annotations[utils.PoolerSpecHashAnnotationName]).Should(Equal(expectedHash))

		// Check the metadata
		Expect(deployment.ObjectMeta.Name).To(Equal(pooler.Name))
		Expect(deployment.ObjectMeta.Namespace).To(Equal(pooler.Namespace))
		Expect(deployment.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.PgbouncerNameLabel:              pooler.Name,
			utils.PodRoleLabelName:                string(utils.PodRolePooler),
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))
		// Check the DeploymentSpec
		Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		Expect(deployment.Spec.Selector.MatchLabels[utils.PgbouncerNameLabel]).To(Equal(pooler.Name))

		// Check the PodTemplateSpec
		podTemplate := deployment.Spec.Template
		Expect(podTemplate.ObjectMeta.Annotations).To(Equal(pooler.Spec.Template.ObjectMeta.Annotations))
		Expect(podTemplate.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.PgbouncerNameLabel:              pooler.Name,
			utils.PodRoleLabelName:                string(utils.PodRolePooler),
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))

		// Check the containers
		Expect(podTemplate.Spec.Containers).ToNot(BeEmpty())
		Expect(podTemplate.Spec.Containers[0].Name).To(Equal("pgbouncer"))
		Expect(podTemplate.Spec.Containers[0].Image).To(Equal(config.Current.PgbouncerImageName))
	})

	It("sets the correct number of replicas", func() {
		pooler.Spec.Instances = ptr.To(int32(3))
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
	})

	It("sets the correct deployment strategy", func() {
		pooler.Spec.DeploymentStrategy = &appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType,
		}
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
	})

	It("creates correct volume mounts", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).ToNot(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(Equal("scratch-data"))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(postgres.ScratchDataDirectory))
	})

	It("creates correct init containers", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())

		Expect(deployment.Spec.Template.Spec.InitContainers).ToNot(BeEmpty())
		Expect(deployment.Spec.Template.Spec.InitContainers[0].Name).To(Equal(specs.BootstrapControllerContainerName))
	})

	It("sets the correct service account name when not specified", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(pooler.Name))
	})

	It("sets the custom service account name when specified", func() {
		customPooler := pooler.DeepCopy()
		customSA := "custom-service-account"
		customPooler.Spec.ServiceAccountName = &customSA
		deployment, err := Deployment(customPooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("custom-service-account"))
	})

	It("sets the correct readiness probe", func() {
		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TimeoutSeconds).To(Equal(int32(5)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port).
			To(Equal(intstr.FromInt32(pgBouncerConfig.PgBouncerPort)))
	})

	It("should correctly set pod resources to the bootstrap init container", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Template: &apiv1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
		}

		dep, err := Deployment(pooler, cluster)
		Expect(err).ToNot(HaveOccurred())
		// check that the init container has the correct resources
		Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
		initResources := dep.Spec.Template.Spec.InitContainers[0].Resources
		Expect(initResources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
		Expect(initResources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("128Mi")))
		Expect(initResources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
		Expect(initResources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
	})

	It("retains user-defined bootstrap-controller resources", func() {
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name: specs.BootstrapControllerContainerName,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2m"),
							corev1.ResourceMemory: resource.MustParse("30Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				}},
			},
		}

		deployment, err := Deployment(pooler, cluster)
		Expect(err).ShouldNot(HaveOccurred())
		var found *corev1.Container
		for i := range deployment.Spec.Template.Spec.InitContainers {
			if deployment.Spec.Template.Spec.InitContainers[i].Name == specs.BootstrapControllerContainerName {
				found = &deployment.Spec.Template.Spec.InitContainers[i]
				break
			}
		}
		Expect(found).ToNot(BeNil())
		Expect(found.Resources.Requests.Cpu().String()).To(Equal("2m"))
		Expect(found.Resources.Requests.Memory().String()).To(Equal("30Mi"))
		Expect(found.Resources.Limits.Cpu().String()).To(Equal("1"))
		Expect(found.Resources.Limits.Memory().String()).To(Equal("100Mi"))
	})
})
