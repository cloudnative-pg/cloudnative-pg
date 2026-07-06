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
	"net"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

	It("retries a transport-level dial timeout to the instance manager (production path)", func() {
		// Mirrors the error chain produced when the finalize status read cannot reach
		// the instance manager: a *net.OpError wrapped by the HTTP client and the
		// reconciler.
		err := fmt.Errorf("while getting status while finalizing: %w",
			fmt.Errorf("while executing http request: %w",
				&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("i/o timeout")}))
		Expect(isNetworkErrorRetryable(err)).To(BeTrue())
	})

	It("retries a transport-level connection error", func() {
		// The bare, unwrapped net.OpError, and a connection refused rather than a
		// timeout: errors.As must match the type directly, without relying on a
		// Timeout()/Temporary() check.
		err := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
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
