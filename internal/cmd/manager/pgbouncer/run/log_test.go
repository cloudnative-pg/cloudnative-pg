/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package run

import (
	"io"
	"os"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/logtest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgbouncer log parsing", func() {
	It("correctly identifies the fields on each record line", func() {
		f, err := os.Open("testdata/pgbouncer.log")
		defer func() {
			_ = f.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		spy := logtest.NewSpy()
		writer := pgBouncerLogWriter{
			Logger: spy,
		}
		_, err = io.Copy(&writer, f)
		Expect(err).ToNot(HaveOccurred())

		// Check if we received the correct records
		Expect(len(spy.Records)).To(Equal(27))

		// Check that we parse every line of the log file
		for _, record := range spy.Records {
			matched, found := record.Attributes["matched"]
			Expect(!found || matched.(bool)).To(BeTrue())

			pgbouncerLog, found := record.Attributes["record"]
			Expect(found).To(BeTrue())
			Expect(record.Message).To(Equal("record"))
			Expect(pgbouncerLog.(pgBouncerLogRecord).Timestamp).ToNot(BeEmpty())
			Expect(pgbouncerLog.(pgBouncerLogRecord).Pid).ToNot(BeEmpty())
			Expect(pgbouncerLog.(pgBouncerLogRecord).Level).ToNot(BeEmpty())
			Expect(pgbouncerLog.(pgBouncerLogRecord).Msg).ToNot(BeEmpty())
		}
	})
})
