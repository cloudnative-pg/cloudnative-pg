/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler type tests", func() {
	It("pgbouncer pools are not paused by default", func() {
		pgbouncer := PgBouncerSpec{}
		Expect(pgbouncer.IsPaused()).To(BeFalse())
	})

	It("pgbouncer pools can be paused", func() {
		trueVal := true
		pgbouncer := PgBouncerSpec{
			Paused: &trueVal,
		}
		Expect(pgbouncer.IsPaused()).To(BeTrue())
	})
})
