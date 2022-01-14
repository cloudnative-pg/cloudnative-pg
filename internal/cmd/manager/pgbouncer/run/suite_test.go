/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package run

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPgbouncer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pgbouncer instance manager tests")
}
