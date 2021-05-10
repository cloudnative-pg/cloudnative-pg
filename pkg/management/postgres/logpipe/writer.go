/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// logMessage is the name of the logger that will export the PostgreSQL
// logs in structured format
const logMessage string = "postgres"

// LogRecordWriter implements the `RecordWriter` interface writing to the
// instance manager logger
type LogRecordWriter struct{}

// Write write the PostgreSQL log record to the instance manager logger
func (writer *LogRecordWriter) Write(record *Record) {
	log.Log.Info(logMessage, "record", record)
}
