/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"bytes"
	"encoding/csv"
	"io"
)

// CSVReadWriter is the interface for structs that are able to receive writes and parse CSV lines
type CSVReadWriter interface {
	io.Writer
	Read() (record []string, err error)
}

// CSVRecordReadWriter wraps a csv.Reader and implements io.Writer.
// It parses CSV lines, that are then read through the csv.Reader.
type CSVRecordReadWriter struct {
	io.Writer
	*csv.Reader
}

// NewCSVRecordReadWriter returns a new CSVRecordReadWriter which parses CSV lines
// with an expected number of fields. It uses a single record for memory efficiency.
func NewCSVRecordReadWriter(fieldsPerRecord int) *CSVRecordReadWriter {
	recordBuffer := new(bytes.Buffer)
	reader := csv.NewReader(recordBuffer)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = fieldsPerRecord
	return &CSVRecordReadWriter{
		Writer: recordBuffer,
		Reader: reader,
	}
}
