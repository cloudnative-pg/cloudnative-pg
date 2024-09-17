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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConvertToPostgresFormat converts timestamps to PostgreSQL time format, if needed.
// e.g. "2006-01-02T15:04:05Z07:00" --> "2006-01-02 15:04:05.000000Z07:00"
// If the conversion fails, the input timestamp is returned as it is.
func ConvertToPostgresFormat(timestamp string) string {
	if t, err := time.Parse(metav1.RFC3339Micro, timestamp); err == nil {
		return t.Format("2006-01-02 15:04:05.000000Z07:00")
	}

	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return t.Format("2006-01-02 15:04:05.000000Z07:00")
	}

	return timestamp
}

// GetCurrentTimestamp returns the current timestamp as a string in RFC3339Micro format
func GetCurrentTimestamp() string {
	t := time.Now()
	return t.Format(metav1.RFC3339Micro)
}

// GetCurrentTimestampWithFormat returns the current timestamp as a string with the specified format
func GetCurrentTimestampWithFormat(format string) string {
	t := time.Now()
	return t.Format(format)
}

// DifferenceBetweenTimestamps returns the time.Duration difference between two timestamps strings in time.RFC3339.
func DifferenceBetweenTimestamps(first, second string) (time.Duration, error) {
	parsedTimestamp, err := time.Parse(metav1.RFC3339Micro, first)
	if err != nil {
		return 0, err
	}

	parsedTimestampTwo, err := time.Parse(metav1.RFC3339Micro, second)
	if err != nil {
		return 0, err
	}

	return parsedTimestamp.Sub(parsedTimestampTwo), nil
}

// ToCompactISO8601 converts a time.Time into a compacted version of the ISO8601 timestamp,
// removing any separators for brevity.
//
// For example:
//
//	Given: 2022-01-02 15:04:05 (UTC)
//	Returns: 20220102150405
//
// This compact format is useful for generating concise, yet human-readable timestamps that
// can serve as suffixes for backup-related objects or any other contexts where space or
// character count might be a concern.
func ToCompactISO8601(t time.Time) string {
	return t.Format("20060102150405")
}
