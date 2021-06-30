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

// LoggingCollectorRecordName is the value of the logger field for logging_collector
const LoggingCollectorRecordName = "postgres"

// Start start a new goroutine running the logging collector core, reading
// from the logging_collector process and translating its content to JSON
func Start() error {
	p := logPipe{
		fileName:        filepath.Join(postgres.LogPath, postgres.LogFileName+".csv"),
		record:          &LoggingRecord{},
		fieldsValidator: LogFieldValidator,
		sourceName:      LoggingCollectorRecordName,
	}
	return p.start()
}

// LogFieldValidator checks if the provided number of fields is valid or not for logging_collector logs
func LogFieldValidator(fields int) *ErrFieldCountExtended {
	if fields != FieldsPerRecord12 && fields != FieldsPerRecord13 {
		// If the number of fields is not recognised return an error
		return &ErrFieldCountExtended{
			Err: fmt.Errorf("invalid number of fields opening logging_collector CSV log stream"),
		}
	}
	return nil
}

// LoggingRecord is used to store all the fields of the logging_collector CSV format
type LoggingRecord struct {
	LogTime              string `json:"log_time"`
	Username             string `json:"user_name"`
	DatabaseName         string `json:"database_name"`
	ProcessID            string `json:"process_id"`
	ConnectionFrom       string `json:"connection_from"`
	SessionID            string `json:"session_id"`
	SessionLineNum       string `json:"session_line_num"`
	CommandTag           string `json:"command_tag"`
	SessionStartTime     string `json:"session_start_time"`
	VirtualTransactionID string `json:"virtual_transaction_id"`
	TransactionID        string `json:"transaction_id"`
	ErrorSeverity        string `json:"error_severity"`
	SQLStateCode         string `json:"sql_state_code"`
	Message              string `json:"message"`
	Detail               string `json:"detail"`
	Hint                 string `json:"hint"`
	InternalQuery        string `json:"internal_query"`
	InternalQueryPos     string `json:"internal_query_pos"`
	Context              string `json:"context"`
	Query                string `json:"query"`
	QueryPos             string `json:"query_pos"`
	Location             string `json:"location"`
	ApplicationName      string `json:"application_name"`
	BackendType          string `json:"backend_type"`
}

// FromCSV store inside the record structure the relative fields
// of the CSV log record.
//
// See https://www.postgresql.org/docs/current/runtime-config-logging.html
// section "19.8.4. Using CSV-Format Log Output".
func (r *LoggingRecord) FromCSV(content []string) {
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

	// Starting from PostgreSQL 13 there is also the BackendType field.
	if len(content) == FieldsPerRecord13 {
		r.BackendType = content[23]
	}
}
