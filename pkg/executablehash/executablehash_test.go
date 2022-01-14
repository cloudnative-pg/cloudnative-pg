/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package executablehash

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Executable hash detection", func() {
	It("detect a hash", func() {
		result, err := Get()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result)).To(Equal(64))
	})
})
