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

package executablehash

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Executable hash detection", func() {
	It("detect a hash", func() {
		result, err := Get()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result).To(HaveLen(64))
	})

	It("retrieves a hash from a given filename", func() {
		const expectedHash = "d6672ee3a93d0d6e3c30bdef89f310799c2f3ab781098a9792040d5541ce3ed3"
		const fileName = "test-hash"

		tempDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(tempDir, fileName), []byte(fileName), 0o600)).To(Succeed())

		result, err := GetByName(filepath.Join(tempDir, fileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(64))
		Expect(result).To(BeEquivalentTo(expectedHash))
	})
})
