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

// PgAuditFieldsPerRecord is the number of fields in a pgaudit log line
const PgAuditFieldsPerRecord int = 9

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
		CSVReadWriter: NewCSVRecordReadWriter(PgAuditFieldsPerRecord),
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

// FromCSV implements the RecordParser interface, parsing a LoggingRecord and its
// pgaudit msg if any
func (r *PgAuditLoggingDecorator) FromCSV(content []string) NamedRecord {
	r.LoggingRecord.FromCSV(content)

	tag, record := getTagAndContent(r.LoggingRecord)
	if tag != "AUDIT" || record == "" {
		return r.LoggingRecord
	}

	_, err := r.CSVReadWriter.Write([]byte(record))
	if err != nil {
		return r.LoggingRecord
	}
	auditContent, err := r.Read()
	if err != nil {
		return r.LoggingRecord
	}

	r.LoggingRecord.Message = ""
	r.Audit.fromCSV(auditContent)
	return r
}

// FromJSON implements the JSONRecordParser interface, parsing a LoggingRecord and its
// pgaudit msg if any
func (r *PgAuditLoggingDecorator) FromJSON(content []byte) (NamedRecord, error) {
	if r, err := r.LoggingRecord.FromJSON(content); err != nil {
		return r, err
	}

	tag, record := getTagAndContent(r.LoggingRecord)
	if tag != "AUDIT" || record == "" {
		return r.LoggingRecord, nil
	}

	_, err := r.CSVReadWriter.Write([]byte(record))
	if err != nil {
		return r.LoggingRecord, nil
	}
	auditContent, err := r.Read()
	if err != nil {
		return r.LoggingRecord, nil
	}

	r.LoggingRecord.Message = ""
	r.Audit.fromCSV(auditContent)
	return r, nil
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
}
