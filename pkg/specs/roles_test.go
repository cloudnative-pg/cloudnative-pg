/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Roles", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("are created with the cluster name", func() {
		serviceAccount := CreateRole(cluster)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
	})
})
