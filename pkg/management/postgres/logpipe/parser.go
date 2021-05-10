/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// RecordWriter is the interface
type RecordWriter interface {
	Write(r *Record)
}

// ErrFieldCountExtended is returned when the CSV line has an invalid number
// of fields
type ErrFieldCountExtended struct {
	Fields []string
	Err    error
}

// Error returns a description of the invalid record
func (err *ErrFieldCountExtended) Error() string {
	buffer, _ := json.Marshal(err.Fields)
	return fmt.Sprintf("invalid fields count: %v", string(buffer))
}

// Cause returns the parent error
func (err *ErrFieldCountExtended) Cause() error {
	return err.Err
}

// streamLogFromCSVFile is a function reading csv lines from an io.Reader and
// writing them to the passed RecordWriter. This function can return
// ErrFieldCountExtended which enrich the csv.ErrFieldCount with the
// decoded invalid line
func streamLogFromCSVFile(inputFile io.Reader, writer RecordWriter) error {
	var (
		content []string
		err     error
		record  Record
	)

	reader := csv.NewReader(inputFile)
	reader.ReuseRecord = true

	// Read the first line outside the loop to validate the number of fields
	if content, err = reader.Read(); err != nil {
		// If the stream is finished, we are done before starting
		if errors.Is(err, io.EOF) {
			return nil
		}

		return err
	}

	// If the number of records is among the expected values write the log and enter the loop
	switch reader.FieldsPerRecord {
	case FieldsPerRecord12, FieldsPerRecord13:
		record.FromCSV(content)
		writer.Write(&record)
	default:
		// If the number of fields is not recognised return an error
		return &ErrFieldCountExtended{
			Fields: content,
			Err:    fmt.Errorf("invalid number of fields opening CSV log stream"),
		}
	}

reader:
	for {
		if content, err = reader.Read(); err != nil {
			switch {
			// If we have an invalid number of fields we enrich the
			// error structure with the parsed CSV line
			case errors.Is(err, csv.ErrFieldCount):
				err = &ErrFieldCountExtended{
					Fields: content,
					Err:    err,
				}

			// If the stream is finished, we are done
			case errors.Is(err, io.EOF):
				break reader
			}

			return err
		}

		record.FromCSV(content)
		writer.Write(&record)
	}

	return nil
}
