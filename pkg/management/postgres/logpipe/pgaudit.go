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

// PgAuditFieldsPerRecord is the number of fields in a pgaudit log line
const PgAuditFieldsPerRecord int = 9

// PgAuditFieldsPerRecordWithRows is the number of fields in a pgaudit log line
// when "pgaudit.log_rows" is set to "on"
const PgAuditFieldsPerRecordWithRows int = 10

// PgAuditRecordName is the value of the logger field for pgaudit
const PgAuditRecordName = "pgaudit"

// PgAuditLoggingDecorator stores all the fields of pgaudit CSV format
type PgAuditLoggingDecorator struct {
	*LoggingRecord
	Audit         *PgAuditRecord `json:"audit,omitempty"`
	CSVReadWriter `json:"-"`
}

// NewPgAuditLoggingDecorator builds PgAuditLoggingDecorator
func NewPgAuditLoggingDecorator() *PgAuditLoggingDecorator {
	return &PgAuditLoggingDecorator{
		LoggingRecord: &LoggingRecord{},
		Audit:         &PgAuditRecord{},
		CSVReadWriter: NewCSVRecordReadWriter(PgAuditFieldsPerRecord, PgAuditFieldsPerRecordWithRows),
	}
}

// GetName implements the NamedRecord interface
func (r *PgAuditLoggingDecorator) GetName() string {
	return PgAuditRecordName
}

func getTagAndContent(record *LoggingRecord) (string, string) {
	if record != nil && tagRegex.MatchString(record.Message) {
		matches := tagRegex.FindStringSubmatch(record.Message)
		return matches[1], matches[2]
	}
	return "", ""
}

// FromCSV implements the CSVRecordParser interface, parsing a LoggingRecord and then
func (r *PgAuditLoggingDecorator) FromCSV(content []string) NamedRecord {
	r.LoggingRecord.FromCSV(content)

	tag, record := getTagAndContent(r.LoggingRecord)
	if tag != "AUDIT" || record == "" {
		return r.LoggingRecord
	}

	_, err := r.Write([]byte(record))
	if err != nil {
		return r.LoggingRecord
	}
	auditContent, err := r.Read()
	if err != nil {
		return r.LoggingRecord
	}

	r.Message = ""
	r.Audit.fromCSV(auditContent)
	return r
}

func (r *PgAuditRecord) fromCSV(auditContent []string) {
	r.AuditType = auditContent[0]
	r.StatementID = auditContent[1]
	r.SubstatementID = auditContent[2]
	r.Class = auditContent[3]
	r.Command = auditContent[4]
	r.ObjectType = auditContent[5]
	r.ObjectName = auditContent[6]
	r.Statement = auditContent[7]
	r.Parameter = auditContent[8]
	if len(auditContent) >= PgAuditFieldsPerRecordWithRows {
		r.Rows = auditContent[9]
	} else {
		r.Rows = ""
	}
}

// PgAuditRecord stores all the fields of a pgaudit log line
type PgAuditRecord struct {
	AuditType      string `json:"audit_type,omitempty"`
	StatementID    string `json:"statement_id,omitempty"`
	SubstatementID string `json:"substatement_id,omitempty"`
	Class          string `json:"class,omitempty"`
	Command        string `json:"command,omitempty"`
	ObjectType     string `json:"object_type,omitempty"`
	ObjectName     string `json:"object_name,omitempty"`
	Statement      string `json:"statement,omitempty"`
	Parameter      string `json:"parameter,omitempty"`
	Rows           string `json:"rows,omitempty"`
}
