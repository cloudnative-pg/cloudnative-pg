/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package logpipe implements reading csv logs from PostgreSQL logging_collector
// (https://www.postgresql.org/docs/current/runtime-config-logging.html) and convert them to JSON.
package logpipe

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// Start start a new goroutine running the logging collector core, reading
// from the logging_collector process and translating its content to JSON
func Start() error {
	if err := fileutils.EnsureDirectoryExist(postgres.LogPath); err != nil {
		return err
	}

	csvPath := filepath.Join(postgres.LogPath, postgres.LogFileName+".csv")
	if _, err := os.Stat(csvPath); err != nil {
		errSysCall := syscall.Mkfifo(csvPath, 0o600)
		if errSysCall != nil {
			return fmt.Errorf("creating log FIFO: %w", errSysCall)
		}
	}

	go loggingCollector(csvPath)

	return nil
}

// loggingCollector will repeatedly try to open the FIFO file where PostgreSQL is writing
// its logs, decode them, and printing using the instance manager logger infrastructure.
func loggingCollector(csvPath string) {
	for {
		if err := collectLogsFromFile(csvPath); err != nil {
			log.Log.Error(err, "Error consuming log stream")
		}
	}
}

// collectLogsFromFile opens (blocking) the FIFO file, then starts reading the csv file line by line
// until the end of the file or an error.
func collectLogsFromFile(csvPath string) error {
	defer func() {
		if condition := recover(); condition != nil {
			log.Log.Info("Recover from panic condition while collecting PostgreSQL logs",
				"condition", condition)
		}
	}()

	f, err := os.OpenFile(csvPath, os.O_RDONLY, 0o600) // #nosec
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			log.Log.Error(err, "Error while closing FIFO file for logs")
			return
		}
	}()

	return streamLogFromCSVFile(f, &LogRecordWriter{})
}
