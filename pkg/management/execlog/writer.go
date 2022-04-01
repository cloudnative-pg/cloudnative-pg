/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package execlog

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// LogWriter implements the `Writer` interface using the logger,
// It uses "Info" as logging level.
type LogWriter struct {
	Logger log.Logger
}

// Write logs the given slice of bytes using the provided Logger.
func (w *LogWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.Logger.Info(string(p))
	}

	return len(p), nil
}
