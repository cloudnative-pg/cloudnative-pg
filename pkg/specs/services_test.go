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

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

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
		Expect(service.Spec.Selector["postgresql"]).To(Equal("clustername"))
	})

	It("create a configured -r service", func() {
		service := CreateClusterReadService(postgresql)
		Expect(service.Name).To(Equal("clustername-r"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector["postgresql"]).To(Equal("clustername"))
	})

	It("create a configured -ro service", func() {
		service := CreateClusterReadOnlyService(postgresql)
		Expect(service.Name).To(Equal("clustername-ro"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector["postgresql"]).To(Equal("clustername"))
		Expect(service.Spec.Selector[ClusterRoleLabelName]).To(Equal(ClusterRoleLabelReplica))
	})

	It("create a configured -rw service", func() {
		service := CreateClusterReadWriteService(postgresql)
		Expect(service.Name).To(Equal("clustername-rw"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector["postgresql"]).To(Equal("clustername"))
		Expect(service.Spec.Selector[ClusterRoleLabelName]).To(Equal(ClusterRoleLabelPrimary))
	})
})
