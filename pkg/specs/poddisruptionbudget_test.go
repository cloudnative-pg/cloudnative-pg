/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POD Disruption Budget specifications", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("have the same name as the PostgreSQL cluster", func() {
		result := CreatePodDisruptionBudget(cluster)
		Expect(result.Name).To(Equal(cluster.Name))
		Expect(result.Namespace).To(Equal(cluster.Namespace))
	})

	It("require a maximum of one unavailable instances", func() {
		result := CreatePodDisruptionBudget(cluster)
		Expect(result.Spec.MaxUnavailable.IntVal).To(Equal(int32(1)))
	})
})
