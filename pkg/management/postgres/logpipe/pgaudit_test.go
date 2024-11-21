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
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgAudit CSV log record", func() {
	Context("Given a CSV record from pgAudit", func() {
		It("fills the fields", func() {
			values := make([]string, PgAuditFieldsPerRecord)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			var r PgAuditRecord
			r.fromCSV(values)
			Expect(r).To(Equal(PgAuditRecord{
				AuditType:      "0",
				StatementID:    "1",
				SubstatementID: "2",
				Class:          "3",
				Command:        "4",
				ObjectType:     "5",
				ObjectName:     "6",
				Statement:      "7",
				Parameter:      "8",
			}))
		})
	})
})

var _ = Describe("PgAudit CVS logging decorator", func() {
	Context("Given a CSV record embedding pgAudit without rows", func() {
		It("fills the fields for PostgreSQL 13", func() { // nolint:dupl
			values := make([]string, FieldsPerRecord12)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			auditValues := make([]string, PgAuditFieldsPerRecord)
			for i := range auditValues {
				auditValues[i] = fmt.Sprintf("A%d", i)
			}
			values[13] = writePgAuditMessage(auditValues)
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(PgAuditRecordName))
			typedResult := result.(*PgAuditLoggingDecorator)
			Expect(*typedResult.LoggingRecord).To(Equal(LoggingRecord{
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
				Message:              "",
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
			Expect(*typedResult.Audit).To(Equal(PgAuditRecord{
				AuditType:      "A0",
				StatementID:    "A1",
				SubstatementID: "A2",
				Class:          "A3",
				Command:        "A4",
				ObjectType:     "A5",
				ObjectName:     "A6",
				Statement:      "A7",
				Parameter:      "A8",
			}))
		})

		It("fills the fields for PostgreSQL 13", func() { // nolint:dupl
			values := make([]string, FieldsPerRecord13)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			auditValues := make([]string, PgAuditFieldsPerRecord)
			for i := range auditValues {
				auditValues[i] = fmt.Sprintf("A%d", i)
			}
			values[13] = writePgAuditMessage(auditValues)
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(PgAuditRecordName))
			typedResult := result.(*PgAuditLoggingDecorator)
			Expect(*typedResult.LoggingRecord).To(Equal(LoggingRecord{
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
				Message:              "",
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
			Expect(*typedResult.Audit).To(Equal(PgAuditRecord{
				AuditType:      "A0",
				StatementID:    "A1",
				SubstatementID: "A2",
				Class:          "A3",
				Command:        "A4",
				ObjectType:     "A5",
				ObjectName:     "A6",
				Statement:      "A7",
				Parameter:      "A8",
			}))
		})
	})

	Context("Given a CSV record embedding pgAudit with rows", func() {
		It("fills the fields for PostgreSQL 13", func() { // nolint:dupl
			values := make([]string, FieldsPerRecord12)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			auditValues := make([]string, PgAuditFieldsPerRecordWithRows)
			for i := range auditValues {
				auditValues[i] = fmt.Sprintf("A%d", i)
			}
			values[13] = writePgAuditMessage(auditValues)
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(PgAuditRecordName))
			typedResult := result.(*PgAuditLoggingDecorator)
			Expect(*typedResult.LoggingRecord).To(Equal(LoggingRecord{
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
				Message:              "",
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
			Expect(*typedResult.Audit).To(Equal(PgAuditRecord{
				AuditType:      "A0",
				StatementID:    "A1",
				SubstatementID: "A2",
				Class:          "A3",
				Command:        "A4",
				ObjectType:     "A5",
				ObjectName:     "A6",
				Statement:      "A7",
				Parameter:      "A8",
				Rows:           "A9",
			}))
		})

		It("fills the fields for PostgreSQL 13", func() { // nolint:dupl
			values := make([]string, FieldsPerRecord13)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			auditValues := make([]string, PgAuditFieldsPerRecordWithRows)
			for i := range auditValues {
				auditValues[i] = fmt.Sprintf("A%d", i)
			}
			values[13] = writePgAuditMessage(auditValues)
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(PgAuditRecordName))
			typedResult := result.(*PgAuditLoggingDecorator)
			Expect(*typedResult.LoggingRecord).To(Equal(LoggingRecord{
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
				Message:              "",
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
			Expect(*typedResult.Audit).To(Equal(PgAuditRecord{
				AuditType:      "A0",
				StatementID:    "A1",
				SubstatementID: "A2",
				Class:          "A3",
				Command:        "A4",
				ObjectType:     "A5",
				ObjectName:     "A6",
				Statement:      "A7",
				Parameter:      "A8",
				Rows:           "A9",
			}))
		})
	})

	Context("Given a CSV record not embedding pgAudit", func() {
		It("fills the fields for PostgreSQL 13", func() {
			values := make([]string, FieldsPerRecord12)
			for i := range values {
				values[i] = fmt.Sprintf("%d", i)
			}
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(LoggingCollectorRecordName))
			typedResult := result.(*LoggingRecord)
			Expect(*typedResult).To(BeEquivalentTo(LoggingRecord{
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
			r := NewPgAuditLoggingDecorator()
			result := r.FromCSV(values)
			Expect(result.GetName()).To(Equal(LoggingCollectorRecordName))
			typedResult := result.(*LoggingRecord)
			Expect(*typedResult).To(BeEquivalentTo(LoggingRecord{
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

var _ = Describe("pgAudit parsing internals", func() {
	When("a message contains a pgAudit formatted record without rows", func() {
		writer := NewCSVRecordReadWriter(PgAuditFieldsPerRecord)
		pgAuditRecord := &PgAuditRecord{}
		validRecords := []*LoggingRecord{
			{Message: "AUDIT: SESSION,1,1,READ,SELECT,,,\"SELECT pg_last_wal_receive_lsn()," +
				" pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()\",<none>"},
			{Message: "AUDIT: SESSION,1,1,DDL,CREATE TABLE,TABLE,public.account,\"create table account\n(" +
				"\n    id int,\n    name text,\n    password text,\n    description text\n);\",<not logged>"},
		}
		It("identifies the message as pgAudit generated", func() {
			for _, record := range validRecords {
				tag, content := getTagAndContent(record)
				Expect(tag).To(BeEquivalentTo("AUDIT"))
				Expect(content).NotTo(BeEmpty())
			}
		})
		It("decodes the message correctly", func() {
			for _, record := range validRecords {
				tag, rawContent := getTagAndContent(record)
				Expect(tag).To(BeEquivalentTo("AUDIT"))
				n, err := writer.Write([]byte(rawContent))
				Expect(err).ShouldNot(HaveOccurred())
				Expect(n).To(BeEquivalentTo(len(rawContent)))
				content, err := writer.Read()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(content).NotTo(BeEmpty())
				pgAuditRecord.fromCSV(content)
				Expect(pgAuditRecord.AuditType).To(BeEquivalentTo("SESSION"))
			}
		})
	})
	When("a message contains a pgAudit formatted record with rows", func() {
		writer := NewCSVRecordReadWriter(PgAuditFieldsPerRecord, PgAuditFieldsPerRecordWithRows)
		pgAuditRecord := &PgAuditRecord{}
		validRecords := []*LoggingRecord{
			{Message: "AUDIT: SESSION,1,1,READ,SELECT,,,\"SELECT pg_last_wal_receive_lsn()," +
				" pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()\",<none>"},
			{Message: "AUDIT: SESSION,1,1,DDL,CREATE TABLE,TABLE,public.account,\"create table account\n(" +
				"\n    id int,\n    name text,\n    password text,\n    description text\n);\",<not logged>,2"},
			{Message: "AUDIT: SESSION,1,1,READ,SELECT,,,\"SELECT pg_last_wal_receive_lsn()," +
				" pg_last_wal_replay_lsn(), pg_is_wal_replay_paused()\",<none>"},
		}
		It("identifies the message as pgAudit generated", func() {
			for _, record := range validRecords {
				tag, content := getTagAndContent(record)
				Expect(tag).To(BeEquivalentTo("AUDIT"))
				Expect(content).NotTo(BeEmpty())
			}
		})
		It("decodes the messages correctly switching between lengths as needed", func() {
			for i, record := range validRecords {
				tag, rawContent := getTagAndContent(record)
				Expect(tag).To(BeEquivalentTo("AUDIT"))
				n, err := writer.Write([]byte(rawContent))
				Expect(err).ShouldNot(HaveOccurred())
				Expect(n).To(BeEquivalentTo(len(rawContent)))
				content, err := writer.Read()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(content).NotTo(BeEmpty())
				pgAuditRecord.fromCSV(content)
				Expect(pgAuditRecord.AuditType).To(BeEquivalentTo("SESSION"))
				if i == 1 {
					Expect(pgAuditRecord.Rows).To(BeEquivalentTo("2"))
				} else {
					Expect(pgAuditRecord.Rows).To(BeEmpty())
				}
			}
		})
	})
})

func writePgAuditMessage(content []string) string {
	buffer := new(bytes.Buffer)
	writer := csv.NewWriter(buffer)
	_ = writer.Write(content)
	Expect(writer.Error()).ShouldNot(HaveOccurred())
	writer.Flush()
	return fmt.Sprintf("AUDIT: %s", strings.TrimSuffix(buffer.String(), "\n"))
}
