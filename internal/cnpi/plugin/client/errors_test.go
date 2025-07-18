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

package client

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("wrapAsPluginErrorIfNeeded", func() {
	It("should return nil when err is nil", func() {
		result := wrapAsPluginErrorIfNeeded(nil)
		Expect(result).ToNot(HaveOccurred())
	})

	It("should return the same error when it already contains a plugin error", func() {
		originalErr := newPluginError("original plugin error")
		wrappedErr := fmt.Errorf("wrapped: %w", originalErr)

		result := wrapAsPluginErrorIfNeeded(wrappedErr)
		Expect(result).To(Equal(wrappedErr))
		Expect(ContainsPluginError(result)).To(BeTrue())
	})

	It("should wrap a non-plugin error as a plugin error", func() {
		originalErr := errors.New("some regular error")

		result := wrapAsPluginErrorIfNeeded(originalErr)
		Expect(result).ToNot(Equal(originalErr))
		Expect(ContainsPluginError(result)).To(BeTrue())
		Expect(result.Error()).To(Equal(originalErr.Error()))
	})

	It("should wrap a nested non-plugin error as a plugin error", func() {
		originalErr := errors.New("base error")
		wrappedErr := fmt.Errorf("context: %w", originalErr)

		result := wrapAsPluginErrorIfNeeded(wrappedErr)
		Expect(result).ToNot(Equal(wrappedErr))
		Expect(ContainsPluginError(result)).To(BeTrue())
		Expect(result.Error()).To(Equal(wrappedErr.Error()))
		Expect(errors.Is(result, originalErr)).To(BeTrue())
	})

	It("should not double-wrap an already wrapped plugin error", func() {
		originalErr := errors.New("base error")
		pluginErr := &pluginError{innerErr: originalErr}
		wrappedPluginErr := fmt.Errorf("additional context: %w", pluginErr)

		result := wrapAsPluginErrorIfNeeded(wrappedPluginErr)
		Expect(result).To(Equal(wrappedPluginErr))
		Expect(ContainsPluginError(result)).To(BeTrue())
	})
})

var _ = Describe("ContainsPluginError", func() {
	It("should return false when err is nil", func() {
		result := ContainsPluginError(nil)
		Expect(result).To(BeFalse())
	})

	It("should return true when error is a direct plugin error", func() {
		pluginErr := newPluginError("test plugin error")

		result := ContainsPluginError(pluginErr)
		Expect(result).To(BeTrue())
	})

	It("should return true when plugin error is wrapped with fmt.Errorf", func() {
		pluginErr := newPluginError("original plugin error")
		wrappedErr := fmt.Errorf("context: %w", pluginErr)

		result := ContainsPluginError(wrappedErr)
		Expect(result).To(BeTrue())
	})

	It("should return true when plugin error is deeply nested", func() {
		pluginErr := newPluginError("base plugin error")
		wrappedOnce := fmt.Errorf("level 1: %w", pluginErr)
		wrappedTwice := fmt.Errorf("level 2: %w", wrappedOnce)

		result := ContainsPluginError(wrappedTwice)
		Expect(result).To(BeTrue())
	})

	It("should return false when error chain contains no plugin error", func() {
		baseErr := errors.New("base error")
		wrappedErr := fmt.Errorf("wrapped: %w", baseErr)

		result := ContainsPluginError(wrappedErr)
		Expect(result).To(BeFalse())
	})

	It("should return true when error chain contains plugin error mixed with other errors", func() {
		baseErr := errors.New("base error")
		pluginErr := &pluginError{innerErr: baseErr}
		wrappedErr := fmt.Errorf("additional context: %w", pluginErr)

		result := ContainsPluginError(wrappedErr)
		Expect(result).To(BeTrue())
	})
})
