/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"time"

	"github.com/lib/pq"
)

// ConvertToPostgresFormat converts timestamps to PostgreSQL time format, if needed.
// e.g. "2006-01-02T15:04:05Z07:00" --> "2006-01-02 15:04:05.000000Z07:00"
// If the conversion fails, the input timestamp is returned as it is.
func ConvertToPostgresFormat(timestamp string) string {
	parsedTimestamp, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return parsedTimestamp.Format("2006-01-02 15:04:05.000000Z07:00")
}

// GetCurrentTimestamp returns the current timestamp as a string in RFC3339 format
func GetCurrentTimestamp() string {
	t := time.Now()
	return t.Format(time.RFC3339)
}

// ParseTargetTime returns the parsed targetTime which is used for point-in-time-recovery
// Currently, we support formats of targetTime as follows:
// YYYY-MM-DD HH24:MI:SS
// YYYY-MM-DD HH24:MI:SS.FF6TZH
// YYYY-MM-DD HH24:MI:SS.FF6TZH:TZM
func ParseTargetTime(currentLocation *time.Location, targetTime string) (time.Time, error) {
	t, err := pq.ParseTimestamp(currentLocation, targetTime)
	if err == nil {
		return t, nil
	}

	return t, err
}
