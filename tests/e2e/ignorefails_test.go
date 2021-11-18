/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// A failing test to verify that our ignore-fails label is correctly ignored
// when evaluating the test reports.
var _ = Describe("ignoreFails on e2e tests", Label(tests.LabelIgnoreFails),
	func() {
		It("generates a failing tests that should be ignored", func() {
			Expect(true).To(BeFalse())
		})
	})
