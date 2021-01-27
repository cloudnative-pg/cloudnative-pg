/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Role binding", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("is created with the same name as the cluster", func() {
		roleBinding := CreateRoleBinding(cluster.ObjectMeta)
		Expect(roleBinding.Name).To(Equal(cluster.Name))
		Expect(roleBinding.Namespace).To(Equal(cluster.Namespace))
	})
})
