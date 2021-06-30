/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package logpipe implements reading csv logs from PostgreSQL logging_collector
// (https://www.postgresql.org/docs/current/runtime-config-logging.html) and convert them to JSON.
package logpipe

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

type logPipe struct {
	fileName        string
	sourceName      string
	record          CSVRecordParser
	fieldsValidator FieldsValidator
}

var consumedLogFiles sync.Map

// FieldsValidator is a function validating the number of fields
// for a specific log line to be parsed
type FieldsValidator func(int) *ErrFieldCountExtended

// Starts a new goroutine running the logging collector core, reading
// from a process logging in CSV to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
func (p *logPipe) start() error {
	_, alreadyStarted := consumedLogFiles.LoadOrStore(p.fileName, true)

	if !alreadyStarted {
		go func() {
			for {
				// check if the directory exists
				if err := fileutils.EnsureDirectoryExist(filepath.Dir(p.fileName)); err != nil {
					log.Log.WithValues("fileName", p.fileName).Error(err,
						"Error checking if the directory exists")
					continue
				}

				if err := fileutils.CreateFifo(p.fileName); err != nil {
					log.Log.WithValues("fileName", p.fileName).Error(err, "Error creating log FIFO")
					continue
				}

				if err := p.collectLogsFromFile(); err != nil {
					log.Log.WithValues("fileName", p.fileName).Error(err, "Error consuming log stream")
				}
			}
		}()
	}

	return nil
}

// collectLogsFromFile opens (blocking) the FIFO file, then starts reading the csv file line by line
// until the end of the file or an error.
func (p *logPipe) collectLogsFromFile() error {
	defer func() {
		if condition := recover(); condition != nil {
			log.Log.Info("Recover from panic condition while collecting PostgreSQL logs",
				"condition", condition, "fileName", p.fileName, "stacktrace", debug.Stack())
		}
	}()

	f, err := os.OpenFile(p.fileName, os.O_RDONLY, 0o600) // #nosec
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			log.Log.Error(err, "Error while closing FIFO file for logs")
			return
		}
	}()

	return p.streamLogFromCSVFile(f, &LogRecordWriter{p.sourceName})
}

// streamLogFromCSVFile is a function reading csv lines from an io.Reader and
// writing them to the passed RecordWriter. This function can return
// ErrFieldCountExtended which enrich the csv.ErrFieldCount with the
// decoded invalid line
func (p *logPipe) streamLogFromCSVFile(inputFile io.Reader, writer RecordWriter) error {
	var (
		content []string
		err     error
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
	if err := p.fieldsValidator(reader.FieldsPerRecord); err != nil {
		err.Fields = content
		return err
	}
	p.record.FromCSV(content)
	writer.Write(p.record)

reader:
	for {
		if content, err = reader.Read(); err != nil {
			switch {
			// If we have an invalid number of fields we enrich the
			// error structure with the parsed CSV line
			case errors.Is(err, csv.ErrFieldCount):
				return &ErrFieldCountExtended{
					Fields:   content,
					Expected: reader.FieldsPerRecord,
					Err:      err,
				}

			// If the stream is finished, we are done
			case errors.Is(err, io.EOF):
				break reader
			default:
				return err
			}
		}

		p.record.FromCSV(content)
		writer.Write(p.record)
	}

	return nil
}
