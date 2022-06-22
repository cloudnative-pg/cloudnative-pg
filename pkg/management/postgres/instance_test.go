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

package postgres

import (
	"github.com/blang/semver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parsing versions", func() {
	It("properly works when version is malformed", func() {
		_, err := parseVersionNum("not-a-version")
		Expect(err).Should(HaveOccurred())
	})

	It("properly works when version is well-formed and >= 10", func() {
		v, err := parseVersionNum("120034")
		Expect(err).To(BeNil())
		Expect(v).To(Equal(&semver.Version{Major: 12, Patch: 34}))
	})

	It("properly works when version is well-formed and < 10", func() {
		v, err := parseVersionNum("090807")
		Expect(err).To(BeNil())
		Expect(v).To(Equal(&semver.Version{Major: 9, Minor: 8, Patch: 7}))
	})
})
