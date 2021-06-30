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
	"syscall"
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
	out, err = os.Create(destination)
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
	return WriteFile(fileName, []byte(contents), 0o666)
}

// WriteFile replace the contents of a certain file
// with a string. If the file doesn't exist, it's created.
// Returns an error status and a flag telling if the file has been
// changed or not.
func WriteFile(fileName string, contents []byte, perm os.FileMode) (changed bool, err error) {
	var exist bool
	exist, err = FileExists(fileName)
	if err != nil {
		return changed, err
	}
	if exist {
		var previousContents []byte
		previousContents, err = ioutil.ReadFile(fileName) // #nosec
		if err != nil {
			err = fmt.Errorf("while reading previous file contents: %w", err)
			return changed, err
		}

		// If nothing changed return immediately
		if bytes.Equal(previousContents, contents) {
			return changed, err
		}
	}

	// Ensure that the directory really exist
	if err := EnsureParentDirectoryExist(fileName); err != nil {
		return false, err
	}

	changed = true

	var out *os.File
	out, err = os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) // #nosec
	if err != nil {
		return changed, err
	}
	defer func() {
		closeError := out.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
	}()

	_, err = out.Write(contents)
	if err != nil {
		return changed, err
	}

	err = out.Sync()
	return changed, err
}

// ReadFile Read source file and output the content as string.
// If the file does not exist, it returns an empty string with no error.
func ReadFile(fileName string) (string, error) {
	exists, err := FileExists(fileName)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	content, err := ioutil.ReadFile(fileName) // #nosec
	if err != nil {
		return "", err
	}

	return string(content), nil
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
	file, err := os.Create(fileName)
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
		err = os.Mkdir(destinationDir, 0o700)
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateFifo invokes the Unix system call Mkfifo, if the given filename exists
func CreateFifo(fileName string) error {
	if _, err := os.Stat(fileName); err != nil {
		return syscall.Mkfifo(fileName, 0o600)
	}
	return nil
}
