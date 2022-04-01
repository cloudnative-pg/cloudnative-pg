/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package logpipe

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils/compatibility"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

type lineHandler func(line []byte)

type lineLogPipe struct {
	fileName string
	handler  lineHandler
}

// Starts a new goroutine running the logging collector core, reading
// from a process logging JSON-line strings to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
func newJSONLineLogPipe(fileName string) *lineLogPipe {
	return &lineLogPipe{
		fileName: fileName,
		handler: func(line []byte) {
			fmt.Println(string(line))
		},
	}
}

// Starts a new goroutine running the logging collector core, reading
// from a process logging raw strings to a file and redirecting its content to stdout in JSON format.
// The goroutine is started just once for a given file.
// All successive calls, that are referencing the same filename, will just check its existence
func newRawLogFile(fileName, name string) *lineLogPipe {
	logger := log.WithName(name).WithValues("source", fileName)

	return &lineLogPipe{
		fileName: fileName,
		handler: func(line []byte) {
			if len(line) != 0 {
				logger.Info(string(line))
			}
		},
	}
}

func (p *lineLogPipe) start() error {
	_, alreadyStarted := consumedLogFiles.LoadOrStore(p.fileName, true)

	if !alreadyStarted {
		go func() {
			for {
				// check if the directory exists
				if err := fileutils.EnsureDirectoryExist(filepath.Dir(p.fileName)); err != nil {
					log.WithValues("fileName", p.fileName).Error(err,
						"Error checking if the directory exists")
					continue
				}

				if err := compatibility.CreateFifo(p.fileName); err != nil {
					log.WithValues("fileName", p.fileName).Error(err, "Error creating log FIFO")
					continue
				}

				if err := p.collectLogsFromFile(); err != nil {
					log.WithValues("fileName", p.fileName).Error(err, "Error consuming log stream")
				}
			}
		}()
	}

	return nil
}

// collectLogsFromFile opens (blocking) the FIFO file, then starts reading the csv file line by line
// until the end of the file or an error.
func (p *lineLogPipe) collectLogsFromFile() error {
	defer func() {
		if condition := recover(); condition != nil {
			log.Info("Recover from panic condition while collecting PostgreSQL logs",
				"condition", condition, "fileName", p.fileName, "stacktrace", debug.Stack())
		}
	}()

	f, err := os.OpenFile(p.fileName, os.O_RDONLY, 0o600) // #nosec
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			log.Error(err, "Error while closing FIFO file for logs")
			return
		}
	}()

	return p.streamLogFromFile(f)
}

// streamLogFromCSVFile is a function reading csv lines from an io.Reader and
// writing them to the passed RecordWriter. This function can return
// ErrFieldCountExtended which enrich the csv.ErrFieldCount with the
// decoded invalid line
func (p *lineLogPipe) streamLogFromFile(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Bytes()
		p.handler(line)
	}

	return scanner.Err()
}
