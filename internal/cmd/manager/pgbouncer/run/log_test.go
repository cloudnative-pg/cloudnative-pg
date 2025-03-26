/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package run

import (
	"io"
	"os"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/logtest"

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
		Expect(spy.Records).To(HaveLen(27))

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
