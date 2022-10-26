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
	"encoding/json"
	"fmt"
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

const maxFieldNumber = FieldsPerRecord14

// LoggingCollectorRecordName is the value of the logger field for logging_collector
const LoggingCollectorRecordName = "postgres"

var clearContentSlice = make([]string, maxFieldNumber)

// CSVLogFieldValidator checks if the provided number of fields is valid or not for logging_collector logs
func CSVLogFieldValidator(fields int) *ErrFieldCountExtended {
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
	LogTime              any    `json:"log_time,omitempty"`
	Username             any    `json:"user_name,omitempty"`
	DatabaseName         any    `json:"database_name,omitempty"`
	ProcessID            any    `json:"process_id,omitempty"`
	ConnectionFrom       any    `json:"connection_from,omitempty"`
	SessionID            any    `json:"session_id,omitempty"`
	SessionLineNum       any    `json:"session_line_num,omitempty"`
	CommandTag           any    `json:"command_tag,omitempty"`
	SessionStartTime     any    `json:"session_start_time,omitempty"`
	VirtualTransactionID any    `json:"virtual_transaction_id,omitempty"`
	TransactionID        any    `json:"transaction_id,omitempty"`
	ErrorSeverity        any    `json:"error_severity,omitempty"`
	SQLStateCode         any    `json:"sql_state_code,omitempty"`
	Message              string `json:"message,omitempty"`
	Detail               any    `json:"detail,omitempty"`
	Hint                 any    `json:"hint,omitempty"`
	InternalQuery        any    `json:"internal_query,omitempty"`
	InternalQueryPos     any    `json:"internal_query_pos,omitempty"`
	Context              any    `json:"context,omitempty"`
	Query                any    `json:"query,omitempty"`
	QueryPos             any    `json:"query_pos,omitempty"`
	Location             any    `json:"location,omitempty"`
	ApplicationName      any    `json:"application_name,omitempty"`
	BackendType          any    `json:"backend_type,omitempty"`
	LeaderPid            any    `json:"leader_pid,omitempty"`
	QueryID              any    `json:"query_id,omitempty"`
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

// FromJSON unmarshals the provided content into the given LoggingRecord,
// returning it and any possible error
func (r *LoggingRecord) FromJSON(content []byte) (NamedRecord, error) {
	r.Clear()
	err := json.Unmarshal(content, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Clear will reset all the LoggingRecord fields to empty string
func (r *LoggingRecord) Clear() NamedRecord {
	return r.FromCSV(clearContentSlice)
}

// GetName implements the NamedRecord interface
func (r *LoggingRecord) GetName() string {
	return LoggingCollectorRecordName
}
