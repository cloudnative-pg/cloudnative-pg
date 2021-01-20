/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL version handling", func() {
	Describe("parsing", func() {
		It("should parse versions < 10", func() {
			Expect(GetPostgresVersionFromTag("9.5.3")).To(Equal(90503))
			Expect(GetPostgresVersionFromTag("9.4")).To(Equal(90400))
		})

		It("should parse versions >= 10", func() {
			Expect(GetPostgresVersionFromTag("10.3")).To(Equal(100003))
			Expect(GetPostgresVersionFromTag("12.3")).To(Equal(120003))
		})

		It("should ignore extra components", func() {
			Expect(GetPostgresVersionFromTag("3.4.3.2.5")).To(Equal(30403))
			Expect(GetPostgresVersionFromTag("10.11.12")).To(Equal(100011))
			Expect(GetPostgresVersionFromTag("9.4_beautiful")).To(Equal(90400))
			Expect(GetPostgresVersionFromTag("11-1")).To(Equal(110000))
		})

		It("should gracefully handle errors", func() {
			_, err := GetPostgresVersionFromTag("")
			Expect(err).To(Not(BeNil()))

			_, err = GetPostgresVersionFromTag("8")
			Expect(err).To(Not(BeNil()))

			_, err = GetPostgresVersionFromTag("9.five")
			Expect(err).To(Not(BeNil()))

			_, err = GetPostgresVersionFromTag("10.old")
			Expect(err).To(Not(BeNil()))
		})
	})

	Describe("major version extraction", func() {
		It("should extract the major version for PostgreSQL >= 10", func() {
			Expect(GetPostgresMajorVersion(100003)).To(Equal(100000))
		})

		It("should extract the major version for PostgreSQL < 10", func() {
			Expect(GetPostgresMajorVersion(90504)).To(Equal(90500))
			Expect(GetPostgresMajorVersion(90400)).To(Equal(90400))
		})
	})

	Describe("detect whenever a version upgrade is possible using the numeric version", func() {
		It("succeed when the major version is the same", func() {
			Expect(IsUpgradePossible(100000, 100003)).To(BeTrue())
			Expect(IsUpgradePossible(90302, 90303)).To(BeTrue())
		})

		It("prevent upgrading to a different major version", func() {
			Expect(IsUpgradePossible(100003, 110003)).To(BeFalse())
			Expect(IsUpgradePossible(90604, 100000)).To(BeFalse())
			Expect(IsUpgradePossible(90503, 900604)).To(BeFalse())
		})
	})

	Describe("detect whenever a version upgrade is possible using the image tag", func() {
		It("succeed when the major version is the same", func() {
			Expect(CanUpgrade("postgres:10.0", "postgres:10.3")).To(BeTrue())
			Expect(CanUpgrade("postgres:9.3.2", "postgres:9.3.3")).To(BeTrue())
		})

		It("prevent using 'latest'", func() {
			Expect(CanUpgrade("postgres:latest", "postgres:10.3")).To(BeFalse())
			Expect(CanUpgrade("postgres:10.0", "postgres:latest")).To(BeFalse())
		})

		It("prevent upgrading to a different major version", func() {
			Expect(CanUpgrade("postgres:10.3", "postgres:11.3")).To(BeFalse())
			Expect(CanUpgrade("postgres:9.6.4", "postgres:10")).To(BeFalse())
			Expect(CanUpgrade("postgres:9.5.3", "postgres:9.6.4")).To(BeFalse())
		})

		It("raise errors when the image tag can't be parsed", func() {
			status, err1 := CanUpgrade("postgres:ten_dot_three", "postgres:11.3")
			Expect(err1).To(Not(BeNil()))
			Expect(status).To(BeFalse())

			status, err2 := CanUpgrade("postgres:10.3", "postgres:eleven_dot_tree")
			Expect(err2).To(Not(BeNil()))
			Expect(status).To(BeFalse())
		})
	})
})
