/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package fileutils contains the utility functions about
// file management
package fileutils

import (
	"io"
	"io/ioutil"
	"os"
)

// AppendStringToFile append the content of the given string to the
// end of the target file prepending new data with a carriage return
func AppendStringToFile(targetFile string, content string) error {
	stream, err := os.OpenFile(
		targetFile,
		os.O_APPEND|os.O_WRONLY, 0600) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeError := stream.Close()
		if closeError != nil {
			err = closeError
		}
	}()

	_, err = stream.WriteString("\n")
	if err != nil {
		return err
	}

	_, err = stream.WriteString(content)
	return err
}

// AppendFile append the content of the source file to the end of the target file
// prepending new data with a carriage return
func AppendFile(targetFile string, sourceFile string) error {
	// TODO: append the file using the Reader / Writer interface,
	// or better avoid appending the file at all
	data, err := ioutil.ReadFile(sourceFile) // #nosec
	if err != nil {
		return err
	}

	stream, err := os.OpenFile(
		targetFile,
		os.O_APPEND|os.O_WRONLY, 0600) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeError := stream.Close()
		if closeError != nil {
			err = closeError
		}
	}()

	_, err = stream.WriteString("\n")
	if err != nil {
		return err
	}

	_, err = stream.Write(data)
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
func CopyFile(source, destination string) error {
	in, err := os.Open(source) // #nosec
	if err != nil {
		return err
	}
	defer func() {
		closeError := in.Close()
		if closeError != nil {
			err = closeError
		}
	}()

	out, err := os.Create(destination)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	err = out.Close()
	return err
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
	_ = file.Close()
	return nil
}
