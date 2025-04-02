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

package pretty

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scanner with increased buffer", func() {
	Context("when scanning large inputs", func() {
		It("should handle inputs larger than default buffer size", func() {
			// Create a large string (larger than default 64KB buffer)
			defaultBufferSize := 64 * 1024
			largeInput := strings.Repeat("x", defaultBufferSize) + "\n" + "second line"
			reader := strings.NewReader(largeInput)

			// Create scanner with our custom function
			scanner := newScanner(defaultBufferSize+1000, reader)

			// Should be able to scan the first line successfully
			Expect(scanner.Scan()).To(BeTrue())
			Expect(scanner.Bytes()).To(HaveLen(defaultBufferSize))

			// And continue to the second line
			Expect(scanner.Scan()).To(BeTrue())
			Expect(string(scanner.Bytes())).To(Equal("second line"))

			// No more lines
			Expect(scanner.Scan()).To(BeFalse())
			Expect(scanner.Err()).To(Succeed())
		})

		It("should fail with default scanner for large inputs", func() {
			// Create a large string (larger than default 64KB buffer)
			defaultBufferSize := 64 * 1024
			largeInput := strings.Repeat("x", defaultBufferSize+1000) + "\n"
			reader := strings.NewReader(largeInput)

			// Use default scanner without buffer increase
			scanner := newScanner(4*1024, reader)

			// This should fail with token too long error
			ok := scanner.Scan()
			if ok {
				// If it somehow succeeds, the line should be truncated
				Expect(len(scanner.Bytes())).To(BeNumerically("<", defaultBufferSize+1000))
			} else {
				// More likely, it fails with a token too long error
				Expect(scanner.Err()).To(HaveOccurred())
				Expect(scanner.Err().Error()).To(ContainSubstring("too long"))
			}
		})

		It("should handle multiple large lines", func() {
			// Create multiple large lines
			largeLineSize := 2 * 1024 * 1024 // 2MB per line
			lines := []string{
				strings.Repeat("a", largeLineSize),
				strings.Repeat("b", largeLineSize),
				strings.Repeat("c", largeLineSize),
			}
			input := strings.Join(lines, "\n")
			reader := strings.NewReader(input)

			// Use our custom scanner
			scanner := newScanner(largeLineSize+1000, reader)

			// Read all lines
			var scannedLines []string
			for scanner.Scan() {
				scannedLines = append(scannedLines, string(scanner.Bytes()))
			}

			// Verify no errors and correct lines read
			Expect(scanner.Err()).To(Succeed())
			Expect(scannedLines).To(HaveLen(3))
			Expect(scannedLines[0]).To(HaveLen(largeLineSize))
			Expect(scannedLines[1]).To(HaveLen(largeLineSize))
			Expect(scannedLines[2]).To(HaveLen(largeLineSize))
		})
	})
})
