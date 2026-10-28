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

package logpipe

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseJSONRecord", func() {
	It("splits a record written by wal-archive into name, level, message and attributes", func() {
		record, err := parseJSONRecord([]byte(`{"level":"error","ts":"2026-10-28T10:00:00.1Z",` +
			`"logger":"wal-archive","msg":"failed to run wal-archive command","logging_pod":"cluster-example-1",` +
			`"error":"exit status 1","walName":"pg_wal/000000010000000000000001","elapsed":0.5,` +
			`"stacktrace":"main.main\n\t/main.go:1"}`))
		Expect(err).ToNot(HaveOccurred())
		Expect(record).To(Equal(jsonRecord{
			name:    "wal-archive",
			level:   "error",
			message: "failed to run wal-archive command",
			keysAndValues: []any{
				"elapsed", 0.5,
				"exception.message", "exit status 1",
				"exception.stacktrace", "main.main\n\t/main.go:1",
				"logging_pod", "cluster-example-1",
				"walName", "pg_wal/000000010000000000000001",
			},
		}))
	})

	It("keeps a record without a logger name unnamed", func() {
		record, err := parseJSONRecord([]byte(`{"level":"info","msg":"hello"}`))
		Expect(err).ToNot(HaveOccurred())
		Expect(record).To(Equal(jsonRecord{level: "info", message: "hello"}))
	})

	It("fails on a line that is not a JSON object", func() {
		_, err := parseJSONRecord([]byte(`not a JSON record`))
		Expect(err).To(HaveOccurred())
	})
})
