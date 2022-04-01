/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package execlog

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Writing to a LogWriter", func() {
	l := LogWriter{Logger: log.GetLogger()}
	When("it is passed nil", func() {
		n, err := l.Write(nil)
		It("does not crash", func() {
			Expect(n).To(Equal(0))
			Expect(err).To(BeNil())
		})
	})
})
