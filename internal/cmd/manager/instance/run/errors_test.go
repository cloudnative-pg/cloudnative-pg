/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
