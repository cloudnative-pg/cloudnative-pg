/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"fmt"
	"path/filepath"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// FieldsPerRecord12 is the number of fields in a CSV log line
// in PostgreSQL 12 or below
const FieldsPerRecord12 int = 23

// FieldsPerRecord13 is the number of fields in a CSV log line
// since PostgreSQL 13
const FieldsPerRecord13 int = 24

// FieldsPerRecord14 is the number of fields in a CSV log line
// since PostgreSQL 14
const FieldsPerRecord14 int = 26

// LoggingCollectorRecordName is the value of the logger field for logging_collector
const LoggingCollectorRecordName = "postgres"

// Start starts a new goroutine running the logging collector core, reading
// from the logging_collector process and translating its content to JSON
func Start() error {
	p := logPipe{
		fileName:        filepath.Join(postgres.LogPath, postgres.LogFileName+".csv"),
		record:          NewPgAuditLoggingDecorator(),
		fieldsValidator: LogFieldValidator,
	}
	return p.start()
}

// LogFieldValidator checks if the provided number of fields is valid or not for logging_collector logs
func LogFieldValidator(fields int) *ErrFieldCountExtended {
	if fields != FieldsPerRecord12 && fields != FieldsPerRecord13 && fields != FieldsPerRecord14 {
		// If the number of fields is not recognised return an error
		return &ErrFieldCountExtended{
			Err: fmt.Errorf("invalid number of fields opening logging_collector CSV log stream"),
		}
	}
	return nil
}

// LoggingRecord is used to store all the fields of the logging_collector CSV format
type LoggingRecord struct {
	LogTime              string `json:"log_time,omitempty"`
	Username             string `json:"user_name,omitempty"`
	DatabaseName         string `json:"database_name,omitempty"`
	ProcessID            string `json:"process_id,omitempty"`
	ConnectionFrom       string `json:"connection_from,omitempty"`
	SessionID            string `json:"session_id,omitempty"`
	SessionLineNum       string `json:"session_line_num,omitempty"`
	CommandTag           string `json:"command_tag,omitempty"`
	SessionStartTime     string `json:"session_start_time,omitempty"`
	VirtualTransactionID string `json:"virtual_transaction_id,omitempty"`
	TransactionID        string `json:"transaction_id,omitempty"`
	ErrorSeverity        string `json:"error_severity,omitempty"`
	SQLStateCode         string `json:"sql_state_code,omitempty"`
	Message              string `json:"message,omitempty"`
	Detail               string `json:"detail,omitempty"`
	Hint                 string `json:"hint,omitempty"`
	InternalQuery        string `json:"internal_query,omitempty"`
	InternalQueryPos     string `json:"internal_query_pos,omitempty"`
	Context              string `json:"context,omitempty"`
	Query                string `json:"query,omitempty"`
	QueryPos             string `json:"query_pos,omitempty"`
	Location             string `json:"location,omitempty"`
	ApplicationName      string `json:"application_name,omitempty"`
	BackendType          string `json:"backend_type,omitempty"`
	LeaderPid            string `json:"leader_pid,omitempty"`
	QueryID              string `json:"query_id,omitempty"`
}

// FromCSV stores inside the record structure the relative fields
// of the CSV log record.
//
// See https://www.postgresql.org/docs/current/runtime-config-logging.html
// section "19.8.4. Using CSV-Format Log Output".
func (r *LoggingRecord) FromCSV(content []string) NamedRecord {
	r.LogTime = content[0]
	r.Username = content[1]
	r.DatabaseName = content[2]
	r.ProcessID = content[3] // integer
	r.ConnectionFrom = content[4]
	r.SessionID = content[5]
	r.SessionLineNum = content[6] // bigint
	r.CommandTag = content[7]
	r.SessionStartTime = content[8]
	r.VirtualTransactionID = content[9]
	r.TransactionID = content[10] // bigint
	r.ErrorSeverity = content[11]
	r.SQLStateCode = content[12]
	r.Message = content[13]
	r.Detail = content[14]
	r.Hint = content[15]
	r.InternalQuery = content[16]
	r.InternalQueryPos = content[17] // integer
	r.Context = content[18]
	r.Query = content[19]
	r.QueryPos = content[20] // integer
	r.Location = content[21]
	r.ApplicationName = content[22]

	// Starting from PostgreSQL 13, there is also the BackendType field.
	// See https://www.postgresql.org/docs/13/runtime-config-logging.html#RUNTIME-CONFIG-LOGGING-CSVLOG
	if len(content) >= FieldsPerRecord13 {
		r.BackendType = content[23]
	}
	// Starting from PostgreSQL 14, there are also the LeaderPid and the QueryID fields.
	// See https://www.postgresql.org/docs/14/runtime-config-logging.html#RUNTIME-CONFIG-LOGGING-CSVLOG
	if len(content) == FieldsPerRecord14 {
		r.LeaderPid = content[24]
		r.QueryID = content[25]
	}
	return r
}

// GetName implements the NamedRecord interface
func (r *LoggingRecord) GetName() string {
	return LoggingCollectorRecordName
}
