/*
Copyright The CloudNativePG Contributors

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
// YYYY-MM-DDTHH24:MI:SSZ            (time.RFC3339)
// YYYY-MM-DDTHH24:MI:SS±TZH:TZM     (time.RFC3339)
// YYYY-MM-DDTHH24:MI:SS             (modified time.RFC3339)
func ParseTargetTime(currentLocation *time.Location, targetTime string) (time.Time, error) {
	t, err := pq.ParseTimestamp(currentLocation, targetTime)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(time.RFC3339, targetTime)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse("2006-01-02T15:04:05", targetTime)
	if err == nil {
		return t, nil
	}

	return t, err
}
