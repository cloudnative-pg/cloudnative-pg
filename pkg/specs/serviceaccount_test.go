/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service accounts", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("create a service account with the cluster name", func() {
		serviceAccount, err := CreateServiceAccount(cluster.ObjectMeta, nil)
		Expect(err).To(BeNil())
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(serviceAccount.Annotations[OperatorManagedSecretsName]).To(Equal("null"))
	})

	It("correctly create the annotation storing the secret names", func() {
		serviceAccount, err := CreateServiceAccount(cluster.ObjectMeta, []string{"one", "two"})
		Expect(err).To(BeNil())
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(serviceAccount.Annotations[OperatorManagedSecretsName]).To(Equal(`["one","two"]`))
	})

	When("the pull secrets are changed", func() {
		It("can detect that the ServiceAccount is needing a refresh", func() {
			serviceAccount, err := CreateServiceAccount(cluster.ObjectMeta, []string{"one", "two"})
			Expect(err).To(BeNil())
			Expect(IsServiceAccountAligned(serviceAccount, []string{"one", "two"})).To(BeTrue())
			Expect(IsServiceAccountAligned(serviceAccount, []string{"one", "two", "three"})).To(BeFalse())
		})
	})

	When("there are secrets not directly managed by the operator", func() {
		It("can detect that the ServiceAccount is needing a refresh", func() {
			serviceAccount, err := CreateServiceAccount(cluster.ObjectMeta, []string{"one", "two"})

			// This image pull secret is not managed by the operator since its name
			// has not been stored inside the annotation inside the ServiceAccount
			serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, v1.LocalObjectReference{
				Name: "token",
			})
			Expect(err).To(BeNil())

			Expect(IsServiceAccountAligned(serviceAccount, []string{"one", "two"})).To(BeTrue())
			Expect(IsServiceAccountAligned(serviceAccount, []string{"one", "two", "three"})).To(BeFalse())
		})
	})
})
