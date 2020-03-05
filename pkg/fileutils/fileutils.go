/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package fileutils contains the utility functions about
// file management
package fileutils

import (
	"io/ioutil"
	"os"
)

// AppendFile append the content of the source file to the end of the target file
// pretending new data with a carriage return
func AppendFile(targetFile string, sourceFile string) error {
	// TODO: append the file using the Reader / Writer interface,
	// or better avoid appending the file at all
	data, err := ioutil.ReadFile(sourceFile) // #nosec
	if err != nil {
		return err
	}

	stream, err := os.OpenFile(
		targetFile,
		os.O_APPEND|os.O_WRONLY, 0600)
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
