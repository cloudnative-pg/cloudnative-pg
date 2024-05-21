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

package fileutils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"syscall"

	"github.com/thoas/go-funk"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

const (
	createFileBlockSize = 262144
	probeFileName       = "_cnpg_probe_"
)

type fileCreatorFunc = func(ctx context.Context, name string, size int) error

// Directory represents a filesystem directory and provides methods to interact
// with it, such as checking for available disk space by attempting to create
// a file of a specified size.
type Directory struct {
	path           string
	createFileFunc fileCreatorFunc
}

// NewDirectory creates and returns a new Directory instance for the specified
// path.
func NewDirectory(path string) *Directory {
	return &Directory{
		path:           path,
		createFileFunc: createFileWithSize,
	}
}

// createFileWithSize creates a file with a certain name and
// a certain size. It will fail if the file already exists.
//
// To allocate the file, the specified number of bytes will
// be written, set to zero.
//
// The code of this function is written after the `XLogFileInitInternal`
// PostgreSQL function, to be found in `src/backend/access/transam/xlog.c`
func createFileWithSize(ctx context.Context, name string, size int) error {
	contextLogger := log.FromContext(ctx).WithValues("probeFileName", name)

	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600) // nolint:gosec
	if err != nil {
		return fmt.Errorf("while opening size probe file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			if err != nil {
				contextLogger.Error(
					closeErr,
					"Detected error while closing probe file while managing a write error",
					"originalError", err)
			} else {
				err = closeErr
			}
		}
	}()

	buf := make([]byte, createFileBlockSize)
	var writtenBytes int

	for writtenBytes < size {
		b, err := f.Write(buf[:min(len(buf), size-writtenBytes)])
		if err != nil {
			return fmt.Errorf("while writing to size probe file: %w", err)
		}
		writtenBytes += b
	}

	return nil
}

// HasSpaceInDirectory checks if there's enough disk space to store a
// file with a specified size inside the directory. It does that
// by using createFileFunc to create such a file in the directory
// and then removing it.
func (d Directory) HasSpaceInDirectory(ctx context.Context, size int) (bool, error) {
	var err error

	probeFileName := path.Join(d.path, probeFileName+funk.RandomString(4))
	contextLogger := log.FromContext(ctx).WithValues("probeFileName", probeFileName)

	defer func() {
		if removeErr := RemoveFile(probeFileName); removeErr != nil {
			if err == nil {
				err = removeErr
			} else {
				contextLogger.Error(
					err,
					"Detected error while removing free disk space probe file",
					"originalError", err)
			}
		}
	}()

	err = d.createFileFunc(ctx, probeFileName, size)
	if IsNoSpaceLeftOnDevice(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// IsNoSpaceLeftOnDevice returns true when there's no more
// space left
func IsNoSpaceLeftOnDevice(err error) bool {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return pathError.Err == syscall.ENOSPC
	}

	return false
}
