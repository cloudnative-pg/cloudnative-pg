/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Release tag extraction", func() {
	It("properly works with expected filename", func() {
		tag := extractTag("postgresql-operator-0.5.0.yaml")
		Expect(tag).To(Equal("0.5.0"))
	})
})

var _ = Describe("Most recent tag", func() {
	It("properly works with release tag", func() {
		err := os.Setenv("VERSION", "1.9.1")
		Expect(err).To(BeNil())
		wd, err := os.Getwd()
		Expect(err).To(BeNil())
		parentDir := filepath.Dir(filepath.Dir(wd))
		tag, err := GetMostRecentReleaseTag(parentDir + "/releases")
		Expect(tag).To(Not(BeEmpty()))
		Expect(tag).To(BeEquivalentTo("1.9.0"))
		Expect(err).To(BeNil())
	})

	It("properly works with dev tag", func() {
		err := os.Setenv("VERSION", "1.9.1-test")
		Expect(err).To(BeNil())
		wd, err := os.Getwd()
		Expect(err).To(BeNil())
		parentDir := filepath.Dir(filepath.Dir(wd))
		tag, err := GetMostRecentReleaseTag(parentDir + "/releases")
		Expect(tag).To(Not(BeEmpty()))
		Expect(tag).To(BeEquivalentTo("1.9.1"))
		Expect(err).To(BeNil())
	})
})

var _ = Describe("Dev tag version check", func() {
	It("returns true when version contains a dev tag", func() {
		err := os.Setenv("VERSION", "100.9.1-test")
		Expect(err).To(BeNil())
		Expect(isDevTagVersion()).To(BeTrue())
	})
	It("returns false when version contains a release tag", func() {
		err := os.Setenv("VERSION", "100.9.1")
		Expect(err).To(BeNil())
		Expect(isDevTagVersion()).To(BeFalse())
	})
})
