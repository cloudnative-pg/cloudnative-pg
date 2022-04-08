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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils/compatibility"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

type lineHandler func(line []byte)

// LineLogPipe a pipe for a given format
type LineLogPipe struct {
	fileName string
	handler  lineHandler

	initialized *concurrency.Executed
	exited      *concurrency.Executed
}

// GetExecutedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (p *LineLogPipe) GetExecutedCondition() *concurrency.Executed {
	return p.initialized
}

// GetExitedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (p *LineLogPipe) GetExitedCondition() *concurrency.Executed {
	return p.exited
}

// NewJSONLineLogPipe returns a logPipe for json format
func NewJSONLineLogPipe(fileName string) *LineLogPipe {
	return &LineLogPipe{
		fileName: fileName,
		handler: func(line []byte) {
			fmt.Println(string(line))
		},
		initialized: concurrency.NewExecuted(),
		exited:      concurrency.NewExecuted(),
	}
}

// NewRawLineLogPipe returns a logPipe for raw output
func NewRawLineLogPipe(fileName, name string) *LineLogPipe {
	logger := log.WithName(name).WithValues("source", fileName)

	return &LineLogPipe{
		fileName: fileName,
		handler: func(line []byte) {
			if len(line) != 0 {
				logger.Info(string(line))
			}
		},
		initialized: concurrency.NewExecuted(),
		exited:      concurrency.NewExecuted(),
	}
}

// Start a new goroutine running the logging collector core, reading
// from a process logging raw strings to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
//nolint:dupl
func (p *LineLogPipe) Start(ctx context.Context) error {
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
func (p *LineLogPipe) collectLogsFromFile(ctx context.Context) error {
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
		errChan <- p.streamLogFromFile(ctx, f)
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
func (p *LineLogPipe) streamLogFromFile(ctx context.Context, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Bytes()
		p.handler(line)
	}

	// If the read timed out probably the channel has been cancelled
	if ctx.Err() != nil {
		return nil
	}

	return scanner.Err()
}
