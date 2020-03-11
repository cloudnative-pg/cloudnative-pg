/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package utils

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Image name comparison", func() {
	It("must consider image names equal when they are exactly the same", func() {
		Expect(IsImageNameEqual("thistest:1", "thistest:1")).To(BeTrue())
	})

	It("must consider image names equal when they are exactly the same apart from prefixes/suffixes", func() {
		Expect(IsImageNameEqual("thistest:1", "docker.io/library/thistest:1")).To(BeTrue())
	})

	It("must consider image names equal even if they differ only by their docker.io/library prefix", func() {
		Expect(IsImageNameEqual("thistest:1", "docker.io/library/thistest:1")).To(BeTrue())
		Expect(IsImageNameEqual("docker.io/library/thistest:1", "thistest:1")).To(BeTrue())
	})

	It("must consider different different images", func() {
		Expect(IsImageNameEqual("thistest:1", "thistest:2")).To(BeFalse())
		Expect(IsImageNameEqual("thistest:2", "thistest:1")).To(BeFalse())
		Expect(IsImageNameEqual("thistest:1", "thistoast:1")).To(BeFalse())
		Expect(IsImageNameEqual("thistoast:1", "thistest:1")).To(BeFalse())
	})
})

var _ = Describe("Image name normalisation", func() {
	It("should avoid completing complete names", func() {
		Expect(NormaliseImageName("docker.io/library/mytest:2.1")).To(Equal("docker.io/library/mytest:2.1"))
	})

	It("should complete image names if they have no prefix", func() {
		Expect(NormaliseImageName("mytest:2.1")).To(Equal("docker.io/library/mytest:2.1"))
	})

	It("should complete image names if they don't specify a registry", func() {
		Expect(NormaliseImageName("library/mytest:2.1")).To(Equal("docker.io/library/mytest:2.1"))
	})

	It("should complete image names if they don't specify a tag", func() {
		Expect(NormaliseImageName("library/mytest")).To(Equal("docker.io/library/mytest:latest"))
	})
})
