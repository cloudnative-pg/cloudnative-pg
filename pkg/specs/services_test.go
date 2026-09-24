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

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Services specification", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusterName",
		},
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:18.0",
		},
	}
	expectedPort := corev1.ServicePort{
		Name:       PostgresContainerName,
		Protocol:   corev1.ProtocolTCP,
		TargetPort: intstr.FromString(PostgresPortName),
		Port:       postgres.ServerPort,
	}

	// shared expected labels
	expectedLabels := map[string]string{
		utils.ClusterLabelName:                cluster.Name,
		utils.KubernetesAppLabelName:          utils.AppName,
		utils.KubernetesAppInstanceLabelName:  cluster.Name,
		utils.KubernetesAppVersionLabelName:   "18",
		utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
		utils.KubernetesAppManagedByLabelName: utils.ManagerName,
	}

	// helper to assert common service properties
	assertService := func(
		service *corev1.Service,
		expectedName string,
		publishNotReady bool,
		selectorKey, selectorValue string,
	) {
		Expect(service.Name).To(Equal(expectedName))
		Expect(service.Labels).To(BeEquivalentTo(expectedLabels))
		Expect(service.Spec.PublishNotReadyAddresses).To(Equal(publishNotReady))
		Expect(service.Spec.Selector[utils.ClusterLabelName]).To(Equal(cluster.Name))
		Expect(service.Spec.Selector[selectorKey]).To(Equal(selectorValue))
		Expect(service.Spec.Ports).To(HaveLen(1))
		Expect(service.Spec.Ports).To(ContainElement(expectedPort))
	}

	It("create a configured -any service", func() {
		service := CreateClusterAnyService(cluster)
		assertService(service, cluster.Name+"-any", true, utils.PodRoleLabelName, string(utils.PodRoleInstance))
	})

	It("create a configured -r service", func() {
		service := CreateClusterReadService(cluster)
		assertService(service, cluster.Name+"-r", false, utils.PodRoleLabelName, string(utils.PodRoleInstance))
	})

	It("create a configured -ro service", func() {
		service := CreateClusterReadOnlyService(cluster)
		assertService(service, cluster.Name+"-ro", false, utils.ClusterInstanceRoleLabelName, ClusterRoleLabelReplica)
	})

	It("create a configured -rw service", func() {
		service := CreateClusterReadWriteService(cluster)
		assertService(service, cluster.Name+"-rw", false, utils.ClusterInstanceRoleLabelName, ClusterRoleLabelPrimary)
	})
})

var _ = Describe("BuildManagedServices", func() {
	var cluster apiv1.Cluster

	BeforeEach(func() {
		cluster = apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Services: &apiv1.ManagedServices{
						Additional: []apiv1.ManagedService{
							{
								SelectorType: apiv1.ServiceSelectorTypeRW,
								ServiceTemplate: apiv1.ServiceTemplateSpec{
									ObjectMeta: apiv1.Metadata{
										Name: "test-service",
										Labels: map[string]string{
											"test-label": "test-value",
										},
										Annotations: map[string]string{
											"test-annotation": "test-value",
										},
									},
									Spec: corev1.ServiceSpec{
										Selector: map[string]string{
											"additional": "true",
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})

	Context("when Managed or Services is nil", func() {
		It("should return nil services", func() {
			cluster.Spec.Managed = nil
			services, err := BuildManagedServices(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).To(BeNil())

			cluster.Spec.Managed = &apiv1.ManagedConfiguration{}
			cluster.Spec.Managed.Services = nil
			services, err = BuildManagedServices(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).To(BeNil())
		})
	})

	Context("when there are no additional managed services", func() {
		It("should return nil services", func() {
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{}
			services, err := BuildManagedServices(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).To(BeNil())
		})
	})

	Context("when there are additional managed services", func() {
		It("should build the services", func() {
			services, err := BuildManagedServices(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).NotTo(BeNil())
			Expect(services).To(HaveLen(1))
			Expect(services[0].ObjectMeta.Name).To(Equal("test-service"))
			Expect(services[0].ObjectMeta.Labels).To(HaveKeyWithValue(utils.KubernetesAppManagedByLabelName, utils.ManagerName))
			Expect(services[0].ObjectMeta.Labels).To(HaveKeyWithValue(utils.IsManagedLabelName, "true"))
			Expect(services[0].ObjectMeta.Labels).To(HaveKeyWithValue("test-label", "test-value"))
			Expect(services[0].ObjectMeta.Annotations).To(HaveKeyWithValue("test-annotation", "test-value"))
			Expect(services[0].Spec.Ports).To(ContainElement(corev1.ServicePort{
				Name:       PostgresContainerName,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromString(PostgresPortName),
				Port:       postgres.ServerPort,
				NodePort:   0,
			}))
		})

		It("should not overwrite the user specified service port with the default one", func() {
			cluster.Spec.Managed.Services.Additional[0].ServiceTemplate.Spec.Ports = []corev1.ServicePort{
				{
					Name:       PostgresContainerName,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(PostgresPortName),
					Port:       postgres.ServerPort,
					NodePort:   5533,
				},
			}
			services, err := BuildManagedServices(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).NotTo(BeNil())
			Expect(services).To(HaveLen(1))
			Expect(services[0].Spec.Ports[0].NodePort).To(Equal(int32(5533)))
		})
	})
})
