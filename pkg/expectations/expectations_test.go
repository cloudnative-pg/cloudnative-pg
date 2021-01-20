/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package expectations

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestExpectations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Expectations test suite")
}
