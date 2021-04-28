/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package log contains the logging subsystem of PGK
package log

import (
	"github.com/go-logr/logr"
)

// Log is the logger that will be used in this package
var Log logr.Logger

// SetLogger will set the backing logr implementation for instance manager.
func SetLogger(logr logr.Logger) {
	Log = logr
}
