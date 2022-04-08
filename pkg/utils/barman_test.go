/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
