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

package volumesnapshot

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Retriable error messages", func() {
	DescribeTable(
		"Retriable error messages",
		func(msg string, isRetriable bool) {
			Expect(isRetriableErrorMessage(msg)).To(Equal(isRetriable))
		},
		Entry("conflict", "Hey, the object has been modified!", true),
		Entry("non-retriable error", "VolumeSnapshotClass not found", false),
		Entry("explicitly retriable error", "Retriable: true, the storage is gone away forever", true),
		Entry("explicitly non-retriable error", "Retriable: false because my pod is working", false),
		Entry("error code 502 - retriable", "RetryAfter: 0s, HTTPStatusCode: 502, RawError: Internal Server Error", true),
		Entry("error code 404 - non retriable", "RetryAfter: 0s, HTTPStatusCode: 404, RawError: Not found", false),
	)
})
