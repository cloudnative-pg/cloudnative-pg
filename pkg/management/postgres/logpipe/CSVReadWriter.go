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

import (
	"bytes"
	"encoding/csv"
	"errors"
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
	allowedFieldsPerRecord []int
}

// Read reads a CSV record from the underlying reader, returns the records or any error encountered
func (r *CSVRecordReadWriter) Read() ([]string, error) {
	record, err := r.Reader.Read()
	if err == nil {
		return record, nil
	}

	var parseError *csv.ParseError
	if !errors.As(err, &parseError) {
		return nil, err
	}

	if !errors.Is(parseError.Err, csv.ErrFieldCount) {
		return nil, err
	}

	for _, allowedFields := range r.allowedFieldsPerRecord {
		if len(record) == allowedFields {
			r.FieldsPerRecord = allowedFields
			return record, nil
		}
	}

	return nil, err
}

// NewCSVRecordReadWriter returns a new CSVRecordReadWriter which parses CSV lines
// with an expected number of fields. It uses a single record for memory efficiency.
// If no fieldsPerRecord are provided, it allows variable fields per record.
// If fieldsPerRecord are provided, it will only allow those numbers of fields per record.
func NewCSVRecordReadWriter(fieldsPerRecord ...int) *CSVRecordReadWriter {
	recordBuffer := new(bytes.Buffer)
	reader := csv.NewReader(recordBuffer)
	reader.ReuseRecord = true

	if len(fieldsPerRecord) == 0 {
		// Allow variable fields per record as we don't have an opinion
		reader.FieldsPerRecord = -1
	} else {
		// We'll optimistically set the first value as the default, this way we'll get an error on the first line too.
		// Leaving this to 0 would allow the first line to pass, setting the
		// fields per record for all the following lines without us checking it.
		reader.FieldsPerRecord = fieldsPerRecord[0]
	}

	return &CSVRecordReadWriter{
		Writer:                 recordBuffer,
		Reader:                 reader,
		allowedFieldsPerRecord: fieldsPerRecord,
	}
}
