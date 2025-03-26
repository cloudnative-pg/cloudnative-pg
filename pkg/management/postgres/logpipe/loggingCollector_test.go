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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL CSV log record", func() {
	Context("Given a CSV record from logging collector", func() {
		It("fills the fields for PostgreSQL 14", func() {
			values := make([]string, FieldsPerRecord14)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			var r LoggingRecord
			r.FromCSV(values)
			Expect(r).To(Equal(LoggingRecord{
				LogTime:              "0",
				Username:             "1",
				DatabaseName:         "2",
				ProcessID:            "3",
				ConnectionFrom:       "4",
				SessionID:            "5",
				SessionLineNum:       "6",
				CommandTag:           "7",
				SessionStartTime:     "8",
				VirtualTransactionID: "9",
				TransactionID:        "10",
				ErrorSeverity:        "11",
				SQLStateCode:         "12",
				Message:              "13",
				Detail:               "14",
				Hint:                 "15",
				InternalQuery:        "16",
				InternalQueryPos:     "17",
				Context:              "18",
				Query:                "19",
				QueryPos:             "20",
				Location:             "21",
				ApplicationName:      "22",
				BackendType:          "23",
				LeaderPid:            "24",
				QueryID:              "25",
			}))
		})
		It("fills the fields for PostgreSQL 13", func() {
			values := make([]string, FieldsPerRecord13)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			var r LoggingRecord
			r.FromCSV(values)
			Expect(r).To(Equal(LoggingRecord{
				LogTime:              "0",
				Username:             "1",
				DatabaseName:         "2",
				ProcessID:            "3",
				ConnectionFrom:       "4",
				SessionID:            "5",
				SessionLineNum:       "6",
				CommandTag:           "7",
				SessionStartTime:     "8",
				VirtualTransactionID: "9",
				TransactionID:        "10",
				ErrorSeverity:        "11",
				SQLStateCode:         "12",
				Message:              "13",
				Detail:               "14",
				Hint:                 "15",
				InternalQuery:        "16",
				InternalQueryPos:     "17",
				Context:              "18",
				Query:                "19",
				QueryPos:             "20",
				Location:             "21",
				ApplicationName:      "22",
				BackendType:          "23",
			}))
		})
		It("fills the fields for PostgreSQL 12 or below", func() {
			values := make([]string, FieldsPerRecord12)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			var r LoggingRecord
			r.FromCSV(values)
			Expect(r).To(Equal(LoggingRecord{
				LogTime:              "0",
				Username:             "1",
				DatabaseName:         "2",
				ProcessID:            "3",
				ConnectionFrom:       "4",
				SessionID:            "5",
				SessionLineNum:       "6",
				CommandTag:           "7",
				SessionStartTime:     "8",
				VirtualTransactionID: "9",
				TransactionID:        "10",
				ErrorSeverity:        "11",
				SQLStateCode:         "12",
				Message:              "13",
				Detail:               "14",
				Hint:                 "15",
				InternalQuery:        "16",
				InternalQueryPos:     "17",
				Context:              "18",
				Query:                "19",
				QueryPos:             "20",
				Location:             "21",
				ApplicationName:      "22",
			}))
		})
	})
})
