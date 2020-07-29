/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Services specification", func() {
	postgresql := v1alpha1.Cluster{
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

	It("create a configured -rw service", func() {
		service := CreateClusterReadWriteService(postgresql)
		Expect(service.Name).To(Equal("clustername-rw"))
		Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
		Expect(service.Spec.Selector["postgresql"]).To(Equal("clustername"))
		Expect(service.Spec.Selector[ClusterRoleLabelName]).To(Equal(ClusterRoleLabelPrimary))
	})
})
