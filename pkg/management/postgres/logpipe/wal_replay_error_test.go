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

var _ = Describe("IsWALReplayErrorLine", func() {
	DescribeTable("correctly identifies WAL replay error lines",
		func(line string, expected bool) {
			Expect(IsWALReplayErrorLine(line)).To(Equal(expected))
		},
		// Positive cases – actual PostgreSQL log messages
		Entry("prev-link error (exact message from PG docs)",
			"FATAL:  record with incorrect prev-link 0/3000000 at 0/3000028",
			true,
		),
		Entry("contrecord error (exact message from PG docs)",
			"LOG:  contrecord is requested by 0/3000000",
			true,
		),
		Entry("prev-link error – uppercase variant",
			"FATAL:  Record With Incorrect Prev-Link 0/3000000 at 0/3000028",
			true,
		),
		Entry("contrecord error – uppercase variant",
			"LOG:  Contrecord Is Requested By 0/3000000",
			true,
		),
		Entry("prev-link error embedded in longer line",
			"2026-01-01 00:00:00 UTC [12345]: FATAL:  record with incorrect prev-link 0/5000020 at 0/5000060",
			true,
		),

		// Negative cases – unrelated log lines
		Entry("normal checkpoint log line", "LOG:  checkpoint complete", false),
		Entry("streaming connected log line", "LOG:  streaming replication successfully connected", false),
		Entry("empty line", "", false),
		Entry("line containing 'record' but not the full pattern",
			"LOG:  invalid record length at 0/3000028: wanted 24, got 0",
			false,
		),
	)
})
