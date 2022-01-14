/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package certs

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCerts(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Certs Suite")
}
