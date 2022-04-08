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

package run

import (
	"errors"
	"io/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("unretryable error", func() {
	It("should work with As", func() {
		err := makeUnretryableError(fs.ErrClosed)
		Expect(errors.As(err, &unretryable{})).To(BeTrue())
	})
	It("should work with Is", func() {
		err := makeUnretryableError(fs.ErrClosed)
		Expect(errors.Is(err, unretryable{fs.ErrClosed})).To(BeTrue())
		Expect(errors.Is(err, unretryable{fs.ErrNotExist})).To(BeFalse())
		Expect(errors.Is(err, unretryable{})).To(BeFalse())
	})
})

var _ = Describe("isRunSubCommandRetryable function", func() {
	It("should not retry unretryable error", func() {
		err := makeUnretryableError(errors.New("generic error"))
		Expect(isRunSubCommandRetryable(err)).To(BeFalse())
	})
	It("should retry everything else", func() {
		err := errors.New("generic error")
		Expect(isRunSubCommandRetryable(err)).To(BeTrue())
	})
})
