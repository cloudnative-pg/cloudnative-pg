//go:build linux || darwin
// +build linux darwin

/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package compatibility provides a layer to cross-compile with other OS than Linux
package compatibility

import (
	"os"

	"golang.org/x/sys/unix"
)

// CreateFifo invokes the Unix system call Mkfifo, if the given filename exists
func CreateFifo(fileName string) error {
	if _, err := os.Stat(fileName); err != nil {
		return unix.Mkfifo(fileName, 0o600)
	}
	return nil
}
