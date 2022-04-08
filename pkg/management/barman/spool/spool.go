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

// Package spool implements a WAL pooler keeping track of which WALs we have archived
package spool

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// ErrorNonExistentFile is returned when the spool tried to work
// on a file which doesn't exist
var ErrorNonExistentFile = fs.ErrNotExist

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

	err := os.Remove(path.Join(spool.spoolDirectory, walFile))
	if err != nil && os.IsNotExist(err) {
		return ErrorNonExistentFile
	}
	return err
}

// Touch ensure that a certain WAL file is included into the spool as an empty file
func (spool *WALSpool) Touch(walFile string) (err error) {
	var f *os.File

	walFile = path.Base(walFile)
	fileName := path.Join(spool.spoolDirectory, walFile)
	if f, err = os.Create(filepath.Clean(fileName)); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		log.Warning("Cannot close empty file, error skipped", "fileName", fileName, "err", err)
	}
	return nil
}

// MoveOut moves out a file from the spool to the destination file
func (spool *WALSpool) MoveOut(walName, destination string) (err error) {
	// We cannot use os.Rename here, as it will not work between different
	// volumes, such as moving files from an EmptyDir volume to the data
	// directory.
	// Given that, we rely on the old strategy to copy stuff around.
	err = fileutils.MoveFile(path.Join(spool.spoolDirectory, walName), destination)
	if err != nil && os.IsNotExist(err) {
		return ErrorNonExistentFile
	}
	return err
}

// FileName gets the name of the file for the given WAL inside the spool
func (spool *WALSpool) FileName(walName string) string {
	return path.Join(spool.spoolDirectory, walName)
}
