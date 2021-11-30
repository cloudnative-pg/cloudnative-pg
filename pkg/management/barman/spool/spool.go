/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package spool implements a WAL pooler keeping track of which WALs we have archived
package spool

import (
	"fmt"
	"os"
	"path"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// WALSpool is a way to keep track of which WAL files were processes from the parallel
// feature and not by PostgreSQL request.
// It works using a directory, under which we create an empty file carrying the name
// of the WAL we archived
type WALSpool struct {
	spoolDirectory string
}

// New create new WAL spool
func New(spoolDirectory string) (*WALSpool, error) {
	if err := fileutils.EnsureDirectoryExist(spoolDirectory); err != nil {
		log.Warning("Cannot create the spool directory", "spoolDirectory", spoolDirectory)
		return nil, fmt.Errorf("while creating spool directory: %w", err)
	}

	return &WALSpool{
		spoolDirectory: spoolDirectory,
	}, nil
}

// Contains checks if a certain file is in the spool or not
func (spool *WALSpool) Contains(walFile string) (bool, error) {
	walFile = path.Base(walFile)
	return fileutils.FileExists(path.Join(spool.spoolDirectory, walFile))
}

// Remove removes a WAL file from the spool. If the WAL file doesn't
// exist an error is returned
func (spool *WALSpool) Remove(walFile string) error {
	walFile = path.Base(walFile)
	return os.Remove(path.Join(spool.spoolDirectory, walFile))
}

// Add ensure that a certain WAL file is included into the spool
func (spool *WALSpool) Add(walFile string) (err error) {
	var f *os.File

	walFile = path.Base(walFile)
	fileName := path.Join(spool.spoolDirectory, walFile)
	if f, err = os.Create(fileName); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		log.Warning("Cannot close empty file, error skipped", "fileName", fileName, "err", err)
	}
	return nil
}
