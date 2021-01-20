/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package fileutils contains the utility functions about
// file management
package fileutils

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

// AppendStringToFile append the content of the given string to the
// end of the target file prepending new data with a carriage return
func AppendStringToFile(targetFile string, content string) (err error) {
	var stream *os.File
	stream, err = os.OpenFile(
		targetFile,
		os.O_APPEND|os.O_WRONLY, 0600) // #nosec
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
		if string(previousContents) == contents {
			return changed, err
		}
	}

	changed = true

	var out *os.File
	out, err = os.Create(fileName)
	if err != nil {
		return changed, err
	}
	defer func() {
		closeError := out.Close()
		if err == nil && closeError != nil {
			err = closeError
		}
	}()

	_, err = io.WriteString(out, contents)
	if err != nil {
		return changed, err
	}

	err = out.Sync()
	return changed, err
}

// ReadFile Read source file and output the content as string
func ReadFile(fileName string) (string, error) {
	if _, err := FileExists(fileName); err != nil {
		return "", err
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
	return os.Chmod(pgData, 0700) // #nosec
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

// FindinFile search for a pattern in a file return true
// if success
func FindinFile(fileName string, pattern string) (bool, error) {
	fileContent, err := ReadFile(fileName)
	if err != nil {
		return false, err
	}

	return strings.Contains(fileContent, pattern), nil
}
