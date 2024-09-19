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

package specs

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodSpecDiff", func() {
	It("returns true for superuser-secret volume", func() {
		Expect(shouldIgnoreCurrentVolume("superuser-secret")).To(BeTrue())
	})

	It("returns true for app-secret volume", func() {
		Expect(shouldIgnoreCurrentVolume("app-secret")).To(BeTrue())
	})

	It("returns false for other volumes", func() {
		Expect(shouldIgnoreCurrentVolume("other-volume")).To(BeFalse())
	})

	It("returns false for empty volume name", func() {
		Expect(shouldIgnoreCurrentVolume("")).To(BeFalse())
	})
})
