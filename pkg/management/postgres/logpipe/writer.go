/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package logpipe

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

const (
	logRecordKey = "record"
)

// RecordWriter is the interface
type RecordWriter interface {
	Write(r NamedRecord)
}

// LogRecordWriter implements the `RecordWriter` interface writing to the
// instance manager logger
type LogRecordWriter struct{}

// Write writes the PostgreSQL log record to the instance manager logger
func (writer *LogRecordWriter) Write(record NamedRecord) {
	log.WithName(record.GetName()).Info(logRecordKey, logRecordKey, record)
}
