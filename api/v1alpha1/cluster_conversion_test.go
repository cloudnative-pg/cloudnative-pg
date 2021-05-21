/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster conversion to v1 cluster", func() {
	It("returns nil error ", func() {
		When("destination cluster is empty", func() {
			src := Cluster{}
			dst := v1.Cluster{}
			err := src.ConvertTo(&dst)
			Expect(err).To(BeNil())
			Expect(dst).ToNot(BeNil())
		})
	})
})

var _ = Describe("Cluster conversion from v1 cluster", func() {
	It("returns nil error ", func() {
		When("source cluster is empty", func() {
			src := v1.Cluster{}
			dst := Cluster{}
			err := dst.ConvertFrom(&src)
			Expect(err).To(BeNil())
			Expect(dst).ToNot(BeNil())
		})
	})
})
