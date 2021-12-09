/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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
