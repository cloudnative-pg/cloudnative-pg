/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LoadEnv", func() {
	const key = "test-env"

	AfterEach(func() {
		Delete(key)
	})

	It("returns a copy of the cached slice", func() {
		original := []string{"first", "second"}
		Store(key, append([]string(nil), original...))

		loaded, err := LoadEnv(key)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(HaveLen(len(original)))
		Expect(loaded).To(Equal(original))
		Expect(loaded).NotTo(BeIdenticalTo(original))

		loaded[0] = "mutated"

		reloaded, err := LoadEnv(key)
		Expect(err).NotTo(HaveOccurred())
		Expect(reloaded[0]).To(Equal(original[0]))
		Expect(reloaded).To(Equal(original))
		Expect(loaded[0]).NotTo(Equal(reloaded[0]))
	})
})
