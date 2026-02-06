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

package volumesnapshot

import (
	"context"
	"errors"
	"fmt"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Retriable error messages", func() {
	DescribeTable(
		"Retriable error messages",
		func(msg string, isRetriable bool) {
			Expect(isCSIErrorMessageRetriable(msg)).To(Equal(isRetriable))
		},
		Entry("conflict", "Hey, the object has been modified!", true),
		Entry("non-retriable error", "VolumeSnapshotClass not found", false),
		Entry("explicitly retriable error", "Retriable: true, the storage is gone away forever", true),
		Entry("explicitly non-retriable error", "Retriable: false because my pod is working", false),
		Entry("error code 502 - retriable", "RetryAfter: 0s, HTTPStatusCode: 502, RawError: Internal Server Error", true),
		Entry("error code 404 - non retriable", "RetryAfter: 0s, HTTPStatusCode: 404, RawError: Not found", false),
		Entry("context deadline exceeded - retriable", "context deadline exceeded waiting for snapshot creation", true),
		Entry("deadline exceeded - retriable", "deadline exceeded during Azure snapshot creation", true),
		Entry("timed out - retriable", "operation timed out for csi-disk-handler", true),
		Entry("OCI conflict 409 - retriable", "Error returned by Blockstorage Service. Http Status Code: 409. Error Code: Conflict.", true),
	)

	Describe("isContextDeadlineExceededError", func() {
		It("detects 'context deadline exceeded' error messages", func() {
			Expect(isContextDeadlineExceededError("context deadline exceeded")).To(BeTrue())
		})

		It("detects 'deadline exceeded' error messages", func() {
			Expect(isContextDeadlineExceededError("deadline exceeded")).To(BeTrue())
		})

		It("detects 'timed out' error messages", func() {
			Expect(isContextDeadlineExceededError("operation timed out")).To(BeTrue())
		})

		It("rejects non-timeout error messages", func() {
			Expect(isContextDeadlineExceededError("not found")).To(BeFalse())
			Expect(isContextDeadlineExceededError("permission denied")).To(BeFalse())
			Expect(isContextDeadlineExceededError("invalid input")).To(BeFalse())
		})
	})
})

var _ = Describe("isNetworkErrorRetryable", func() {
	It("recognizes server timeout errors", func() {
		err := apierrs.NewServerTimeout(schema.GroupResource{}, "test", 1)
		Expect(isNetworkErrorRetryable(err)).To(BeTrue())
	})

	It("recognizes conflict errors", func() {
		err := apierrs.NewConflict(schema.GroupResource{}, "test", nil)
		Expect(isNetworkErrorRetryable(err)).To(BeTrue())
	})

	It("recognizes internal errors", func() {
		err := apierrs.NewInternalError(fmt.Errorf("test error"))
		Expect(isNetworkErrorRetryable(err)).To(BeTrue())
	})

	It("recognizes context deadline exceeded errors", func() {
		err := context.DeadlineExceeded
		Expect(isNetworkErrorRetryable(err)).To(BeTrue())
	})

	It("does not retry on not found errors", func() {
		err := apierrs.NewNotFound(schema.GroupResource{}, "test")
		Expect(isNetworkErrorRetryable(err)).To(BeFalse())
	})

	It("does not retry on random errors", func() {
		err := errors.New("random error")
		Expect(isNetworkErrorRetryable(err)).To(BeFalse())
	})
})
