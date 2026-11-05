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
	"runtime/debug"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
)

type lineHandler func(line []byte)

// LineLogPipe a pipe for a given format
type LineLogPipe struct {
	fileName string
	handler  lineHandler
	openFlag int

	initialized *concurrency.Executed
	exited      *concurrency.Executed
}

// GetInitializedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (p *LineLogPipe) GetInitializedCondition() *concurrency.Executed {
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
		openFlag:    os.O_RDONLY,
		initialized: concurrency.NewExecuted(),
		exited:      concurrency.NewExecuted(),
	}
}

// WithNonBlockingOpen makes the pipe open the log FIFO with O_NONBLOCK, so
// the open(2) call never blocks waiting for a writer to connect. With this
// flag, a read that reaches EOF means "no writer is connected" rather than
// "the stream ended": the pipe backs off and retries until a writer shows
// up. This is what bootstrap (initdb, join, restore) needs, where nothing
// may ever write to some of the log FIFOs and a blocking open would park
// the goroutine in the kernel indefinitely.
func (p *LineLogPipe) WithNonBlockingOpen() *LineLogPipe {
	p.openFlag = os.O_RDONLY | nonBlockFlag
	return p
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
		openFlag:    os.O_RDONLY,
		initialized: concurrency.NewExecuted(),
		exited:      concurrency.NewExecuted(),
	}
}

// Start a new goroutine running the logging collector core, reading
// from a process logging raw strings to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
//
//nolint:dupl
func (p *LineLogPipe) Start(ctx context.Context) error {
	filenameLog := log.FromContext(ctx).WithValues("fileName", p.fileName)
	defer filenameLog.Info("Exited log pipe")
	go func() {
		defer p.exited.Broadcast()
		// Broadcast the initialization error if the context is canceled before
		// the initialization is done
		defer func() {
			p.initialized.BroadcastError(ctx.Err())
		}()
		for {
			// If the context has been cancelled, let's avoid starting reading
			// again from the log file
			if err := ctx.Err(); err != nil {
				return
			}

			// check if the directory exists
			if err := fileutils.EnsureParentDirectoryExists(p.fileName); err != nil {
				filenameLog.Error(err, "Error checking if the directory exists")
				waitBeforeRetry(ctx)
				continue
			}
			if err := ensureLogFifo(filenameLog, p.fileName); err != nil {
				waitBeforeRetry(ctx)
				continue
			}
			p.initialized.Broadcast()

			if err := p.collectLogsFromFile(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				filenameLog.Error(err, "Error consuming log stream")
				waitBeforeRetry(ctx)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

// collectLogsFromFile opens the FIFO file, then starts reading it line by
// line until the end of the file or an error. Unless the pipe is configured
// with WithNonBlockingOpen, the open(2) call blocks until a writer
// connects.
func (p *LineLogPipe) collectLogsFromFile(ctx context.Context) error {
	filenameLog := log.FromContext(ctx).WithValues("fileName", p.fileName)

	defer func() {
		if condition := recover(); condition != nil {
			filenameLog.Info("Recover from panic condition while collecting PostgreSQL logs",
				"condition", condition, "stacktrace", debug.Stack())
		}
	}()

	f, err := fileutils.OpenFileAsync(ctx, p.fileName, p.openFlag, 0o600)
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

// streamLogFromFile is a function reading lines from the given FIFO file
// and passing them to the handler until the end of the file or an error.
func (p *LineLogPipe) streamLogFromFile(ctx context.Context, inputFile io.Reader) error {
	for {
		scanner := bufio.NewScanner(inputFile)
		scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			p.handler(line)
		}

		// If the read timed out probably the channel has been cancelled
		if ctx.Err() != nil {
			return nil
		}

		if scanner.Err() != nil {
			return scanner.Err()
		}

		// EOF with a blocking open means the last writer closed the FIFO:
		// the stream is over. With a non-blocking open it means no writer
		// is connected: back off and scan again, keeping the FIFO open so
		// a writer connecting right away is not dropped. The scanner must
		// be recreated, as bufio latches EOF on the old one.
		if p.openFlag&nonBlockFlag == 0 {
			return nil
		}
		waitBeforeRetry(ctx)
	}
}
