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

// Package logpipe implements reading csv logs from PostgreSQL logging_collector
// (https://www.postgresql.org/docs/current/runtime-config-logging.html) and convert them to JSON.
package logpipe

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/concurrency"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils/compatibility"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// LogPipe creates a pipe for a given file
type LogPipe struct {
	fileName        string
	record          CSVRecordParser
	fieldsValidator FieldsValidator

	initialized *concurrency.Executed
	exited      *concurrency.Executed
}

var tagRegex = regexp.MustCompile(`(?s)(?P<Tag>^[a-zA-Z]+): (?P<Record>.*)$`)

// FieldsValidator is a function validating the number of fields
// for a specific log line to be parsed
type FieldsValidator func(int) *ErrFieldCountExtended

// NewLogPipe returns a new LogPipe
func NewLogPipe() *LogPipe {
	return &LogPipe{
		fileName:        filepath.Join(postgres.LogPath, postgres.LogFileName+".csv"),
		record:          NewPgAuditLoggingDecorator(),
		fieldsValidator: LogFieldValidator,

		initialized: concurrency.NewExecuted(),
		exited:      concurrency.NewExecuted(),
	}
}

// GetInitializedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (p *LogPipe) GetInitializedCondition() *concurrency.Executed {
	return p.initialized
}

// GetExitedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (p *LogPipe) GetExitedCondition() *concurrency.Executed {
	return p.exited
}

// Start a new goroutine running the logging collector core, reading
// from a process logging in CSV to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
//nolint:dupl
func (p *LogPipe) Start(ctx context.Context) error {
	filenameLog := log.FromContext(ctx).WithValues("fileName", p.fileName)
	defer filenameLog.Info("Exited log pipe")
	go func() {
		defer p.exited.Broadcast()
		for {
			// If the context has been cancelled, let's avoid starting reading
			// again from the log file
			if err := ctx.Err(); err != nil {
				return
			}

			// check if the directory exists
			if err := fileutils.EnsureDirectoryExist(filepath.Dir(p.fileName)); err != nil {
				filenameLog.Error(err, "Error checking if the directory exists")
				continue
			}

			if err := compatibility.CreateFifo(p.fileName); err != nil {
				filenameLog.Error(err, "Error creating log FIFO")
				continue
			}
			p.initialized.Broadcast()

			if err := p.collectLogsFromFile(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				filenameLog.Error(err, "Error consuming log stream")
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

// collectLogsFromFile opens (blocking) the FIFO file, then starts reading the csv file line by line
// until the end of the file or an error.
func (p *LogPipe) collectLogsFromFile(ctx context.Context) error {
	filenameLog := log.FromContext(ctx).WithValues("fileName", p.fileName)

	defer func() {
		if condition := recover(); condition != nil {
			filenameLog.Info("Recover from panic condition while collecting PostgreSQL logs",
				"condition", condition, "stacktrace", debug.Stack())
		}
	}()

	f, err := fileutils.OpenFileAsync(ctx, p.fileName, os.O_RDONLY, 0o600)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			filenameLog.Error(err, "Error while closing FIFO file for logs")
		}
	}()

	errChan := make(chan error, 1)
	// Ensure we terminate our read operations when
	// the cancellation signal happened
	go func() {
		defer close(errChan)
		errChan <- p.streamLogFromCSVFile(ctx, f, &LogRecordWriter{})
	}()
	select {
	case <-ctx.Done():
		filenameLog.Info("Terminating log reading process")
		err := f.SetDeadline(time.Now())
		if err != nil {
			filenameLog.Error(err,
				"Error while setting the deadline for log reading. The instance manager may not refresh "+
					"until a new log line is read")
		}
		return ctx.Err()
	case err := <-errChan:
		return err
	}
}

// streamLogFromCSVFile is a function reading csv lines from an io.Reader and
// writing them to the passed RecordWriter. This function can return
// ErrFieldCountExtended which enrich the csv.ErrFieldCount with the
// decoded invalid line
func (p *LogPipe) streamLogFromCSVFile(ctx context.Context, inputFile io.Reader, writer RecordWriter) error {
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

		// If the read timed out probably the channel has been cancelled
		if ctx.Err() != nil {
			return nil
		}

		return err
	}

	// If the number of records is among the expected values write the log and enter the loop
	if err := p.fieldsValidator(reader.FieldsPerRecord); err != nil {
		err.Fields = content
		return err
	}
	writer.Write(p.record.FromCSV(content))

reader:
	for {
		// If the context has been cancelled, let's avoid reading another
		// line
		if ctx.Err() != nil {
			return ctx.Err()
		}

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

			case ctx.Err() != nil:
				break reader

			default:
				return err
			}
		}

		writer.Write(p.record.FromCSV(content))
	}

	return nil
}
