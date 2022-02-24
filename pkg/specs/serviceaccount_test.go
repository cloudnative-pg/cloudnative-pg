/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	v1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service accounts", func() {
	It("create a service account with the cluster name", func() {
		sa := &v1.ServiceAccount{}
		err := UpdateServiceAccount(nil, sa)
		Expect(err).To(BeNil())
		Expect(sa.Annotations[OperatorManagedSecretsName]).To(Equal("null"))
	})

	It("correctly create the annotation storing the secret names", func() {
		sa := &v1.ServiceAccount{}
		err := UpdateServiceAccount([]string{"one", "two"}, sa)
		Expect(err).To(BeNil())
		Expect(sa.Annotations[OperatorManagedSecretsName]).To(Equal(`["one","two"]`))
	})

	When("the pull secrets are changed", func() {
		It("can detect that the ServiceAccount is needing a refresh", func() {
			sa := &v1.ServiceAccount{}
			err := UpdateServiceAccount([]string{"one", "two"}, sa)
			Expect(err).To(BeNil())
			Expect(IsServiceAccountAligned(sa, []string{"one", "two"})).To(BeTrue())
			Expect(IsServiceAccountAligned(sa, []string{"one", "two", "three"})).To(BeFalse())
		})
	})

	When("there are secrets not directly managed by the operator", func() {
		It("can detect that the ServiceAccount is needing a refresh", func() {
			sa := &v1.ServiceAccount{}
			err := UpdateServiceAccount([]string{"one", "two"}, sa)

			// This image pull secret is not managed by the operator since its name
			// has not been stored inside the annotation inside the ServiceAccount
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, v1.LocalObjectReference{
				Name: "token",
			})
			Expect(err).To(BeNil())

			Expect(IsServiceAccountAligned(sa, []string{"one", "two"})).To(BeTrue())
			Expect(IsServiceAccountAligned(sa, []string{"one", "two", "three"})).To(BeFalse())
		})
	})
})
