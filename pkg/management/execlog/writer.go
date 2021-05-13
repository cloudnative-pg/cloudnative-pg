/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package execlog

import "github.com/go-logr/logr"

// LogWriter implements the `Writer` interface using the logger,
// It uses "Info" as logging level.
type LogWriter struct {
	Logger logr.Logger
}

// Write logs the given slice of bytes using the provided Logger.
func (w *LogWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.Logger.Info(string(p))
	}

	return len(p), nil
}
