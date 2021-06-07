/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("image name management", func() {
	It("should normalize image names", func() {
		Expect(NewReference("postgres").GetNormalizedName()).To(
			Equal("docker.io/library/postgres:latest"))
		Expect(NewReference("enterprisedb/postgres").GetNormalizedName()).To(
			Equal("docker.io/enterprisedb/postgres:latest"))
		Expect(NewReference("localhost:5000/postgres").GetNormalizedName()).To(
			Equal("localhost:5000/postgres:latest"))
		Expect(NewReference("registry.localhost:5000/postgres:14.4").GetNormalizedName()).To(
			Equal("registry.localhost:5000/postgres:14.4"))
		Expect(NewReference("quay.io/test/postgres:34").GetNormalizedName()).To(
			Equal("quay.io/test/postgres:34"))
	})

	It("should extract tag names", func() {
		Expect(GetImageTag("postgres")).To(Equal("latest"))
		Expect(GetImageTag("postgres:34.3")).To(Equal("34.3"))
		Expect(GetImageTag("postgres:13@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866")).
			To(Equal("13"))
	})

	It("should not extract a tag name", func() {
		Expect(GetImageTag("postgres@sha256:cff94dd382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866")).
			To(BeEmpty())
	})
})
