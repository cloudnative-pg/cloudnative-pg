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

package utils

import (
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isRetryableExecError", func() {
	Context("when error is nil", func() {
		It("should return false", func() {
			Expect(isRetryableExecError(nil)).To(BeFalse())
		})
	})

	Context("when error contains proxy error messages", func() {
		It("should return true for 'proxy error'", func() {
			err := errors.New("proxy error from localhost:9443")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for 'error dialing backend'", func() {
			err := errors.New("error dialing backend: proxy error")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})
	})

	Context("when error contains HTTP 500 messages", func() {
		It("should return true for '500 Internal Server Error'", func() {
			err := errors.New("code 500: 500 Internal Server Error")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for 'Internal error occurred'", func() {
			err := errors.New("Internal error occurred: something went wrong")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})
	})

	Context("when error contains network issues", func() {
		It("should return true for 'connection refused'", func() {
			err := errors.New("dial tcp: connection refused")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for 'connection reset'", func() {
			err := errors.New("read tcp: connection reset by peer")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for 'i/o timeout'", func() {
			err := errors.New("i/o timeout")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for 'TLS handshake timeout'", func() {
			err := errors.New("TLS handshake timeout")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for dial tcp errors", func() {
			err := errors.New("dial tcp 10.0.0.1:443: no route to host")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})
	})

	Context("when error is a Kubernetes API error", func() {
		It("should return true for InternalError", func() {
			err := apierrors.NewInternalError(errors.New("internal error"))
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for ServerTimeout", func() {
			err := apierrors.NewServerTimeout(schema.GroupResource{}, "get", 1)
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for Timeout", func() {
			err := apierrors.NewTimeoutError("timeout", 1)
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for ServiceUnavailable", func() {
			err := apierrors.NewServiceUnavailable("service unavailable")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})

		It("should return true for TooManyRequests", func() {
			err := apierrors.NewTooManyRequests("too many requests", 1)
			Expect(isRetryableExecError(err)).To(BeTrue())
		})
	})

	Context("when error is not retryable", func() {
		It("should return false for NotFound errors", func() {
			err := apierrors.NewNotFound(schema.GroupResource{}, "test")
			Expect(isRetryableExecError(err)).To(BeFalse())
		})

		It("should return false for command execution errors", func() {
			err := errors.New("command terminated with exit code 1")
			Expect(isRetryableExecError(err)).To(BeFalse())
		})

		It("should return false for generic errors", func() {
			err := errors.New("some other error")
			Expect(isRetryableExecError(err)).To(BeFalse())
		})

		It("should return false for ErrorContainerNotFound", func() {
			Expect(isRetryableExecError(ErrorContainerNotFound)).To(BeFalse())
		})
	})

	Context("when error matches the AKS proxy failure pattern", func() {
		It("should return true for the exact error from AKS failures", func() {
			err := errors.New("error dialing backend: proxy error from " +
				"localhost:9443 while dialing 10.224.0.5:10250, code 500: 500 Internal Server Error")
			Expect(isRetryableExecError(err)).To(BeTrue())
		})
	})
})
