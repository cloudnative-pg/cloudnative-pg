/*
Copyright 2019-2022 The CloudNativePG Contributors

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
