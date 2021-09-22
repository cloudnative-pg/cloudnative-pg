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
	When("destination cluster is empty", func() {
		It("returns nil error ", func() {
			src := Cluster{}
			dst := v1.Cluster{}
			err := src.ConvertTo(&dst)
			Expect(err).To(BeNil())
			Expect(dst).ToNot(BeNil())
		})
	})
})

var _ = Describe("Cluster conversion from v1 cluster", func() {
	When("source cluster is empty", func() {
		It("returns nil error ", func() {
			src := v1.Cluster{}
			dst := Cluster{}
			err := dst.ConvertFrom(&src)
			Expect(err).To(BeNil())
			Expect(dst).ToNot(BeNil())
		})
	})
	When("source cluster specifies the certificates", func() {
		It("returns nil error ", func() {
			src := v1.Cluster{Spec: v1.ClusterSpec{
				Certificates: &v1.CertificatesConfiguration{
					ServerCASecret:  "test-server-ca",
					ServerTLSSecret: "test-server-tls",
				},
			}}
			dst := Cluster{}
			err := dst.ConvertFrom(&src)
			Expect(err).To(BeNil())
		})
	})
})
