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
		Expect(NormaliseImageName("postgres")).To(
			Equal("docker.io/library/postgres:latest"))
		Expect(NormaliseImageName("quay.io/test/postgres:34")).To(
			Equal("quay.io/test/postgres:34"))
	})

	It("should extract tag names", func() {
		Expect(GetImageTag("postgres")).To(Equal("latest"))
		Expect(GetImageTag("postgres:34.3")).To(Equal("34.3"))
	})
})
