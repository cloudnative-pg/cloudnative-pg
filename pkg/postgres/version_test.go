/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
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

	Describe("detect whenever a version upgrade is possible", func() {
		It("succeed when the major version is the same", func() {
			Expect(IsUpgradePossible(100000, 100003)).To(BeTrue())
			Expect(IsUpgradePossible(90302, 90303)).To(BeTrue())
		})

		It("prevent from upgrading to a different major version", func() {
			Expect(IsUpgradePossible(100003, 110003)).To(BeFalse())
			Expect(IsUpgradePossible(90604, 100000)).To(BeFalse())
			Expect(IsUpgradePossible(90503, 900604)).To(BeFalse())
		})
	})
})
