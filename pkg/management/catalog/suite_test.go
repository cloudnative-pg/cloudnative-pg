/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package catalog

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCatalog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Barman catalog test suite")
}
