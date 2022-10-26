/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
	Context("Given a JSON record from logging collector", func() {
		It("fills the fields for PostgreSQL 15", func() {
			content := "{\"log_time\":\"2022-10-26 06:50:03.375 UTC\",\"user_name\":\"USER\"," +
				"\"database_name\":\"DB\",\"process_id\":\"31\",\"connection_from\":\"SOURCE_IP\"," +
				"\"session_id\":\"6358d89b.1f\",\"session_line_num\":\"1\",\"command_tag\":\"\"," +
				"\"session_start_time\":\"2022-10-26 06:50:03 UTC\"," +
				"\"virtual_transaction_id\":\"VIRTUAL_TRANSACTION_ID\"," +
				"\"transaction_id\":\"0\",\"error_severity\":\"LOG\",\"sql_state_code\":\"00000\"," +
				"\"message\":\"ending log output to stderr\",\"detail\":\"\"," +
				"\"hint\":\"Future log output will go to log destination \\\"csvlog\\\".\"," +
				"\"internal_query\":\"INTERNAL_QUERY\",\"internal_query_pos\":\"INTERNAL_QUERY_POS\"," +
				"\"context\":\"CONTEXT\",\"query\":\"QUERY\",\"query_pos\":\"42\",\"location\":\"LOCATION\"," +
				"\"application_name\":\"APP_NAME\",\"backend_type\":\"postmaster\"," +
				"\"leader_pid\":\"\",\"query_id\":\"0\"}"
			var r LoggingRecord
			_, err := r.FromJSON([]byte(content))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(Equal(LoggingRecord{
				LogTime:              "2022-10-26 06:50:03.375 UTC",
				Username:             "USER",
				DatabaseName:         "DB",
				ProcessID:            "31",
				ConnectionFrom:       "SOURCE_IP",
				SessionID:            "6358d89b.1f",
				SessionLineNum:       "1",
				CommandTag:           "",
				SessionStartTime:     "2022-10-26 06:50:03 UTC",
				VirtualTransactionID: "VIRTUAL_TRANSACTION_ID",
				TransactionID:        "0",
				ErrorSeverity:        "LOG",
				SQLStateCode:         "00000",
				Message:              "ending log output to stderr",
				Detail:               "",
				Hint:                 "Future log output will go to log destination \"csvlog\".",
				InternalQuery:        "INTERNAL_QUERY",
				InternalQueryPos:     "INTERNAL_QUERY_POS",
				Context:              "CONTEXT",
				Query:                "QUERY",
				QueryPos:             "42",
				Location:             "LOCATION",
				ApplicationName:      "APP_NAME",
				BackendType:          "postmaster",
				LeaderPid:            "",
				QueryID:              "0",
			}))
		})
		It("cleans up fields between runs for PostgreSQL 15", func() {
			firstLogLine := "{\"log_time\":\"2022-10-26 06:50:03.375 UTC\"}"
			var r LoggingRecord
			_, err := r.FromJSON([]byte(firstLogLine))
			Expect(err).NotTo(HaveOccurred())
			Expect(r.LogTime).To(Equal("2022-10-26 06:50:03.375 UTC"))
			secondLogLine := "{\"user_name\":\"USER\"}"
			_, err = r.FromJSON([]byte(secondLogLine))
			Expect(err).NotTo(HaveOccurred())
			Expect(r.Username).To(Equal("USER"))
			Expect(r.LogTime).To(Equal(""))
		})
	})
})
