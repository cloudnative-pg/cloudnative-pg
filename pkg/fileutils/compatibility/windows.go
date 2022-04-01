//go:build windows
// +build windows

/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package compatibility provides a layer to cross-compile with other OS than Linux
package compatibility

import "fmt"

// CreateFifo fakes function for cross-compiling compatibility
func CreateFifo(fileName string) error {
	panic(fmt.Sprintf("function CreateFifo() should not be used in Windows"))
	return nil
}
