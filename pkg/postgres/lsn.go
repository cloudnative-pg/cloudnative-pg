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

// Package postgres contains the function covering the PostgreSQL
// integrations and the relative data types
package postgres

import (
	"fmt"
	"strconv"
	"strings"
)

// LSN is a string composed by two hexadecimal numbers, separated by "/"
type LSN string

// Less compares two LSNs
func (lsn LSN) Less(other LSN) bool {
	p1, err := lsn.Parse()
	if err != nil {
		return false
	}

	p2, err := other.Parse()
	if err != nil {
		return false
	}

	return p1 < p2
}

// Parse an LSN in its components
func (lsn LSN) Parse() (int64, error) {
	components := strings.Split(string(lsn), "/")
	if len(components) != 2 {
		return 0, fmt.Errorf("error parsing LSN %s", lsn)
	}

	// Segment is unsigned int 32, so we parse using 64 bits to avoid overflow on sign bit
	segment, err := strconv.ParseInt(components[0], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing LSN %s: %w", lsn, err)
	}

	// Segment is unsigned int 32, so we parse using 64 bits to avoid overflow on sign bit
	displacement, err := strconv.ParseInt(components[1], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing LSN %s: %w", lsn, err)
	}

	return (segment << 32) + displacement, nil
}
