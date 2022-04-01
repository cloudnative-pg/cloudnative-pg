/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parsing policy", func() {
	It("must properly parse a correct policy", func() {
		Expect(ParsePolicy("30w")).To(BeEquivalentTo("RECOVERY WINDOW OF 30 WEEKS"))
		Expect(ParsePolicy("10w")).To(BeEquivalentTo("RECOVERY WINDOW OF 10 WEEKS"))
		Expect(ParsePolicy("7w")).To(BeEquivalentTo("RECOVERY WINDOW OF 7 WEEKS"))
		Expect(ParsePolicy("7d")).To(BeEquivalentTo("RECOVERY WINDOW OF 7 DAYS"))
	})

	It("must complain with a wrong policy", func() {
		_, err := ParsePolicy("30")
		Expect(err).ToNot(BeNil())

		_, err = ParsePolicy("www")
		Expect(err).ToNot(BeNil())

		_, err = ParsePolicy("00d")
		Expect(err).ToNot(BeNil())
	})
})

var _ = Describe("converting map to barman tags format", func() {
	It("returns an empty slice, if map is missing", func() {
		Expect(MapToBarmanTagsFormat("test", nil)).To(BeEmpty())
	})

	It("works properly, given a map of tags", func() {
		tags := map[string]string{"retentionDays": "90days"}
		Expect(MapToBarmanTagsFormat("test", tags)).To(BeEquivalentTo([]string{"test", "retentionDays,90days"}))
	})
})
