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

// Package fileutils contains the utility functions about
// file management
package fileutils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// excludedPathsFromRestore contains a list of files that should not be included into the restore process
var excludedPathsFromRestore = []string{
	"core.*",
	"pgsql_tmp*",
	"postgresql.auto.conf.tmp",
	"current_logfiles.tmp",
	"pg_internal.init",
	"postmaster.pid",
	"postmaster.opts",
	"recovery.conf",
	"recovery.signal",
	"standby.signal",
	"log/*",
	"pg_dynshmem/*",
	"pg_log/*",
	"pg_notify/*",
	"pg_replslot/*",
	"pg_serial/*",
	"pg_snapshots/*",
	"pg_stat_tmp/*",
	"pg_subtrans/*",
}

// AppendStringToFile append the content of the given string to the
// end of the target file prepending new data with a carriage return
func AppendStringToFile(targetFile string, content string) (err error) {
	var stream *os.File
	stream, err = os.OpenFile(
		targetFile,
		os.O_APPEND|os.O_WRONLY, 0o600) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeError := stream.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
	}()

	_, err = stream.WriteString("\n")
	if err != nil {
		return err
	}

	_, err = stream.WriteString(content)
	if err != nil {
		return err
	}

	err = stream.Sync()
	return err
}

// FileExists check if a file exists, and return an error otherwise
func FileExists(fileName string) (bool, error) {
	if _, err := os.Stat(fileName); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CopyFile copy a file from a location to another one
func CopyFile(source, destination string) (err error) {
	// Ensure that the directory really exist
	if err := EnsureParentDirectoryExist(destination); err != nil {
		return err
	}

	// Copy the file
	var in *os.File
	in, err = os.Open(source) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeError := in.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
	}()

	var out *os.File
	out, err = os.Create(filepath.Clean(destination))
	if err != nil {
		return err
	}
	defer func() {
		closeError := out.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
	}()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Sync()
}

// WriteStringToFile replace the contents of a certain file
// with a string. If the file doesn't exist, it's created.
// Returns an error status and a flag telling if the file has been
// changed or not.
func WriteStringToFile(fileName string, contents string) (changed bool, err error) {
	return WriteFileAtomic(fileName, []byte(contents), 0o666)
}

// WriteLinesToFile replace the contents of a certain file
// with a slice of lines. If the file doesn't exist, it's created.
// Returns an error status and a flag telling if the file has been
// changed or not.
func WriteLinesToFile(fileName string, lines []string) (changed bool, err error) {
	var data []byte
	if len(lines) > 0 {
		data = []byte(strings.Join(lines, "\n") + "\n")
	}
	return WriteFileAtomic(fileName, data, 0o666)
}

// WriteFileAtomic atomically replace the content of a file.
// If the file doesn't exist, it's created.
// Returns an error status and a flag telling if the file has been
// changed or not.
func WriteFileAtomic(fileName string, contents []byte, perm os.FileMode) (bool, error) {
	exist, err := FileExists(fileName)
	if err != nil {
		return false, err
	}
	if exist {
		var previousContents []byte
		previousContents, err = os.ReadFile(fileName) // #nosec
		if err != nil {
			err = fmt.Errorf("while reading previous file contents: %w", err)
			return false, err
		}

		// If nothing changed return immediately
		if bytes.Equal(previousContents, contents) {
			return false, nil
		}
	}

	// Ensure that the directory really exist
	if err := EnsureParentDirectoryExist(fileName); err != nil {
		return false, err
	}

	var out *os.File
	fileNameTmp := fmt.Sprintf("%s_%v", fileName, time.Now().Unix())
	out, err = os.OpenFile(fileNameTmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) // #nosec
	if err != nil {
		return false, err
	}
	defer func() {
		closeError := out.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
		if exists, err := FileExists(fileNameTmp); exists || err != nil {
			_ = os.Remove(fileNameTmp)
		}
	}()

	_, err = out.Write(contents)
	if err != nil {
		return false, err
	}

	err = out.Sync()
	if err != nil {
		return false, err
	}
	err = os.Rename(fileNameTmp, fileName)

	return err == nil, err
}

// ReadFile reads source file and output the content as bytes.
// If the file does not exist, it returns an empty string with no error.
func ReadFile(fileName string) ([]byte, error) {
	exists, err := FileExists(fileName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	content, err := os.ReadFile(fileName) // #nosec
	if err != nil {
		return nil, err
	}

	return content, nil
}

// ReadFileLines reads source file and output the content as a slice of strings.
// If the file does not exist, it returns an empty slice with no error.
func ReadFileLines(fileName string) (lines []string, err error) {
	exists, err := FileExists(fileName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	readFile, err := os.Open(fileName) // #nosec
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := readFile.Close()
		if closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	fileScanner := bufio.NewScanner(readFile)
	fileScanner.Split(bufio.ScanLines)

	for fileScanner.Scan() {
		lines = append(lines, fileScanner.Text())
	}

	return lines, nil
}

// EnsurePgDataPerms ensure PGDATA has 0700 permissions, which are
// required for PostgreSQL to successfully startup
func EnsurePgDataPerms(pgData string) error {
	_, err := os.Stat(pgData)
	if err != nil {
		return err
	}
	return os.Chmod(pgData, 0o700) // #nosec
}

// CreateEmptyFile create an empty file or return an error if
// the file already exist
func CreateEmptyFile(fileName string) error {
	file, err := os.Create(filepath.Clean(fileName))
	if err != nil {
		return err
	}
	return file.Close()
}

// EnsureParentDirectoryExist check if the directory containing a certain file
// exist or not, and if is not existent will create the directory using
// 0700 as permissions bits
func EnsureParentDirectoryExist(fileName string) error {
	destinationDir := filepath.Dir(fileName)
	return EnsureDirectoryExists(destinationDir)
}

// EnsureDirectoryExists check if the passed directory exists or not, and if
// it doesn't exist, create it using 0700 as permissions bits
func EnsureDirectoryExists(destinationDir string) error {
	if _, err := os.Stat(destinationDir); os.IsNotExist(err) {
		err = os.MkdirAll(destinationDir, 0o700)
		if err != nil {
			return err
		}
	}

	return nil
}

// MoveFile moves a file from a source path to its destination by copying
// the source file to the destination and then removing it from the original
// location.
// This will work between different volumes too.
func MoveFile(sourcePath, destPath string) (err error) {
	var inputFile, outputFile *os.File

	inputFile, err = os.Open(sourcePath) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeErr := inputFile.Close()
		if closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	outputFile, err = os.Create(filepath.Clean(destPath))
	if err != nil {
		return err
	}
	defer func() {
		closeErr := outputFile.Close()
		if closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(outputFile, inputFile)
	if err != nil {
		return err
	}

	err = os.Remove(sourcePath)
	return err
}

// RemoveDirectoryContent removes all the files and directories inside the provided path.
// The directory itself is preserved.
func RemoveDirectoryContent(dir string) (err error) {
	names, err := GetDirectoryContent(dir)
	if err != nil {
		return
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return
		}
	}
	return
}

// RemoveFile removes a specified file. Also works if a directory is empty.
func RemoveFile(fileName string) error {
	err := os.Remove(fileName)
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

// RemoveDirectory remove the directory and all its content
func RemoveDirectory(dir string) error {
	if err := RemoveDirectoryContent(dir); err != nil {
		return err
	}
	return RemoveFile(dir)
}

// GetDirectoryContent return a slice of string with the name of the files
// in the dir directory
func GetDirectoryContent(dir string) (files []string, err error) {
	directory, err := os.Open(dir) // #nosec
	if err != nil {
		return
	}
	defer func() {
		closeErr := directory.Close()
		if closeErr != nil {
			err = closeErr
		}
	}()

	const readAllNames = -1
	files, err = directory.Readdirnames(readAllNames)

	return
}

// RemoveFiles deletes the files and directories specified by the filePaths patterns
// relative to the basePath. If a pattern ends with "/*", it implies that all the
// contents of the directory (not the directory itself) matching the pattern should
// be removed. If a pattern does not end with "/*", then the files matching the
// pattern will be removed.
//
// Parameters:
// - ctx: A context used for logging
// - basePath: The root directory where the filePaths are applied.
// - filePaths: List of relative paths or patterns to be removed.
//
// Returns:
// - error: Any error encountered during the removal process, or nil if the operation was successful.
//
// Example:
// basePath: "/path/to/directory"
// filePaths: ["file1.txt", "subdir/*"]
// This would remove "/path/to/directory/file1.txt" and the "path/to/directory/subdir" folder
func RemoveFiles(ctx context.Context, basePath string, filePaths []string) error {
	contextLogger := log.FromContext(ctx)

	for _, pattern := range filePaths {
		if len(pattern) >= 2 && pattern[len(pattern)-2:] == "/*" {
			dirPath := filepath.Join(basePath, pattern[:len(pattern)-2])
			dirExists, err := FileExists(dirPath)
			if err != nil {
				return err
			}
			if dirExists {
				contextLogger.Debug("Removing directory", "dirPath", dirPath)
				if err := RemoveDirectoryContent(dirPath); err != nil {
					return err
				}
			}
			continue
		}

		matches, err := filepath.Glob(filepath.Join(basePath, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			contextLogger.Debug("Removing file", "fileName", match)
			if err := RemoveFile(match); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveRestoreExcludedFiles removes files and directories that are excluded during a restore operation.
// It leverages the RemoveFiles function, using a predefined list of paths that are meant to be excluded.
//
// Parameters:
// - ctx: A context used for logging.
// - basePath: The root path from which the exclusions should be applied.
//
// Returns:
// - error: Any error encountered during the removal process, or nil if the operation was successful.
func RemoveRestoreExcludedFiles(ctx context.Context, basePath string) error {
	return RemoveFiles(ctx, basePath, excludedPathsFromRestore)
}

// MoveDirectoryContent moves a directory from a source path to its destination by copying
// the source files or subdirectories recursively to the destination.
// Once this is done it deletes the files from the original location.
func MoveDirectoryContent(sourceDirectory, destinationDirectory string) error {
	var err error
	var fileNames []string

	// if something fails we remove any copied files if they exist
	defer func() {
		if err != nil {
			_ = RemoveDirectoryContent(destinationDirectory)
		}
	}()

	fileNames, err = GetDirectoryContent(sourceDirectory)
	if err != nil {
		return err
	}
	// we first copy the files without deleting them, this is to avoid inconsistent states
	for _, fileName := range fileNames {
		// We check if the file is a directory, if it is we need to recursively copy it
		fileInfo, err := os.Stat(filepath.Join(sourceDirectory, fileName))
		if err != nil {
			return err
		}

		if fileInfo.IsDir() {
			err := EnsureDirectoryExists(filepath.Join(destinationDirectory, fileName))
			if err != nil {
				return err
			}
			err = MoveDirectoryContent(filepath.Join(sourceDirectory, fileName), filepath.Join(destinationDirectory, fileName))
			if err != nil {
				return err
			}
		} else {
			err = CopyFile(filepath.Join(sourceDirectory, fileName), filepath.Join(destinationDirectory, fileName))
			if err != nil {
				return err
			}
		}
	}

	// we finish by removing the original folder content.
	// we don't assign it to the err variable to avoid triggering defer cleanup logic in case of errors
	return RemoveDirectoryContent(sourceDirectory)
}

// GetFileSize returns the size of a file or an error
func GetFileSize(fileName string) (int64, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return stat.Size(), nil
}

// OpenFileAsync opens a file exiting in case the given context.Context
// is closed while waiting for the OpenFile to terminate, this can be useful with FIFO files,
// as Open will not return until the File is not opened with write permissions by another process or goroutine.
func OpenFileAsync(ctx context.Context, fileName string, flag int, perm os.FileMode) (f *os.File, err error) {
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		f, err = os.OpenFile(fileName, flag, perm) // #nosec
		errChan <- err
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return f, err
	}
}
