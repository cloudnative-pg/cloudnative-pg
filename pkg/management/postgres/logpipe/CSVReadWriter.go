/*
Copyright 2019-2022 The CloudNativePG Contributors

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
