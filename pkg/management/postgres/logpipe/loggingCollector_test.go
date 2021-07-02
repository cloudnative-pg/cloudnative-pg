/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL CSV log record", func() {
	Context("Given a CSV record from logging collector", func() {
		It("fills the fields for PostgreSQL 13", func() {
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
				BackendType:          "",
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
	})
})
