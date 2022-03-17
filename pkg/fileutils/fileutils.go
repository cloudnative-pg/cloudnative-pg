/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package fileutils contains the utility functions about
// file management
package fileutils

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

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
		previousContents, err = ioutil.ReadFile(fileName) // #nosec
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

	content, err := ioutil.ReadFile(fileName) // #nosec
	if err != nil {
		return nil, err
	}

	return content, nil
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
	return EnsureDirectoryExist(destinationDir)
}

// EnsureDirectoryExist check if the passed directory exist or not, and if
// it doesn't exist will create it using 0700 as permissions bits
func EnsureDirectoryExist(destinationDir string) error {
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
