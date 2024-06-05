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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Services specification", func() {
	postgresql := apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: "clustername",
		},
	}

	It("create a configured -any service", func() {
		service := CreateClusterAnyService(postgresql)
		Expect(service.Name).To(Equal("clustername-any"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeTrue())
		Expect(service.Spec.Selector[utils.ClusterLabelName]).To(Equal("clustername"))
		Expect(service.Spec.Selector[utils.PodRoleLabelName]).To(Equal(string(utils.PodRoleInstance)))
	})

	It("create a configured -r service", func() {
		service := CreateClusterReadService(postgresql)
		Expect(service.Name).To(Equal("clustername-r"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector[utils.ClusterLabelName]).To(Equal("clustername"))
		Expect(service.Spec.Selector[utils.PodRoleLabelName]).To(Equal(string(utils.PodRoleInstance)))
	})

	It("create a configured -ro service", func() {
		service := CreateClusterReadOnlyService(postgresql)
		Expect(service.Name).To(Equal("clustername-ro"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector[utils.ClusterLabelName]).To(Equal("clustername"))
		Expect(service.Spec.Selector[utils.ClusterRoleLabelName]).To(Equal(ClusterRoleLabelReplica))
	})

	It("create a configured -rw service", func() {
		service := CreateClusterReadWriteService(postgresql)
		Expect(service.Name).To(Equal("clustername-rw"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector[utils.ClusterLabelName]).To(Equal("clustername"))
		Expect(service.Spec.Selector[utils.ClusterRoleLabelName]).To(Equal(ClusterRoleLabelPrimary))
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
			Expect(len(services)).To(HaveLen(1))
			Expect(services[0].ObjectMeta.Name).To(Equal("test-service"))
			Expect(services[0].ObjectMeta.Labels).To(HaveKeyWithValue(utils.IsManagedLabelName, "true"))
			Expect(services[0].ObjectMeta.Labels).To(HaveKeyWithValue("test-label", "test-value"))
			Expect(services[0].ObjectMeta.Annotations).To(HaveKeyWithValue("test-annotation", "test-value"))
		})
	})
})
