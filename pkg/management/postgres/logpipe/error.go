/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package logpipe

import (
	"encoding/json"
	"fmt"
)

// ErrFieldCountExtended is returned when the CSV line has an invalid number
// of fields
type ErrFieldCountExtended struct {
	Fields   []string
	Err      error
	Expected int
}

// Error returns a description of the invalid record
func (err *ErrFieldCountExtended) Error() string {
	buffer, _ := json.Marshal(err.Fields)
	return fmt.Sprintf("invalid fields count, got %d, expected %d: %v",
		len(err.Fields),
		err.Expected,
		string(buffer))
}

// Cause returns the parent error
func (err *ErrFieldCountExtended) Cause() error {
	return err.Err
}
