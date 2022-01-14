/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Base type mappings for secrets", func() {
	It("correctly map nil values", func() {
		Expect(SecretKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := SecretKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})

var _ = Describe("Base type mappings for configmaps", func() {
	It("correctly map nil values", func() {
		Expect(ConfigMapKeySelectorToCore(nil)).To(BeNil())
	})

	It("correctly map non-nil values", func() {
		selector := ConfigMapKeySelector{
			LocalObjectReference: LocalObjectReference{
				Name: "thisName",
			},
			Key: "thisKey",
		}

		Expect(selector.Name).To(Equal("thisName"))
		Expect(selector.Key).To(Equal("thisKey"))
	})
})
