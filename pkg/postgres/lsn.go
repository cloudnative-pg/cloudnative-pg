/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package postgres contains the function covering the PostgreSQL
// integrations and the relative data types
package postgres

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
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
		return 0, errors.Errorf("Error parsing LSN: %s", lsn)
	}

	segment, err := strconv.ParseInt(components[0], 16, 32)
	if err != nil {
		return 0, errors.Errorf("Error parsing LSN: %s", lsn)
	}

	displacement, err := strconv.ParseInt(components[1], 16, 32)
	if err != nil {
		return 0, errors.Errorf("Error parsing LSN: %s", lsn)
	}

	return (segment << 32) + displacement, nil
}
