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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

type pgControlDataKey = string

const (
	// PgControlDataKeyLatestCheckpointTimelineID is the
	// latest checkpoint's TimeLineID pg_controldata entry
	PgControlDataKeyLatestCheckpointTimelineID pgControlDataKey = "Latest checkpoint's TimeLineID"

	// PgControlDataKeyREDOWALFile is the latest checkpoint's
	// REDO WAL file pg_controldata entry
	PgControlDataKeyREDOWALFile pgControlDataKey = "Latest checkpoint's REDO WAL file"

	// PgControlDataKeyDatabaseSystemIdentifier is the database
	// system identifier pg_controldata entry
	PgControlDataKeyDatabaseSystemIdentifier pgControlDataKey = "Database system identifier"

	// PgControlDataKeyLatestCheckpointREDOLocation is the latest
	// checkpoint's REDO location pg_controldata entry
	PgControlDataKeyLatestCheckpointREDOLocation pgControlDataKey = "Latest checkpoint's REDO location"

	// PgControlDataKeyTimeOfLatestCheckpoint is the time
	// of latest checkpoint pg_controldata entry
	PgControlDataKeyTimeOfLatestCheckpoint pgControlDataKey = "Time of latest checkpoint"
)

// ParsePgControldataOutput parses a pg_controldata output into a map of key-value pairs
func ParsePgControldataOutput(data string) map[string]string {
	pairs := make(map[string]string)
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		key, value, done := strings.Cut(line, ":")
		if !done {
			continue
		}
		pairs[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return pairs
}

// PgControldataTokenContent contains the data needed to properly create a shutdown token
type PgControldataTokenContent struct {
	// Latest checkpoint's TimeLineID
	LatestCheckpointTimelineID string `json:"latestCheckpointTimelineID,omitempty"`

	// Latest checkpoint's REDO WAL file
	REDOWALFile string `json:"redoWalFile,omitempty"`

	// Database system identifier
	DatabaseSystemIdentifier string `json:"databaseSystemIdentifier,omitempty"`

	// Latest checkpoint's REDO location
	LatestCheckpointREDOLocation string `json:"latestCheckpointREDOLocation,omitempty"`

	// Time of latest checkpoint
	TimeOfLatestCheckpoint string `json:"timeOfLatestCheckpoint,omitempty"`

	// The version of the operator that created the token
	OperatorVersion string `json:"operatorVersion,omitempty"`
}

// ErrInvalidShutdownToken is raised when the shutdown checkpoint token
// is not valid
type ErrInvalidShutdownToken struct {
	err    error
	reason string
}

func (e *ErrInvalidShutdownToken) Error() string {
	return fmt.Sprintf("invalid shutdown token format (%s): %s", e.reason, e.err.Error())
}

func (e *ErrInvalidShutdownToken) Unwrap() error {
	return e.err
}

// CreateShutdownToken translates a parsed pgControlData into a JSON token
func CreateShutdownToken(pgDataMap map[string]string) (string, error) {
	content := PgControldataTokenContent{
		LatestCheckpointTimelineID:   pgDataMap[PgControlDataKeyLatestCheckpointTimelineID],
		REDOWALFile:                  pgDataMap[PgControlDataKeyREDOWALFile],
		DatabaseSystemIdentifier:     pgDataMap[PgControlDataKeyDatabaseSystemIdentifier],
		LatestCheckpointREDOLocation: pgDataMap[PgControlDataKeyLatestCheckpointREDOLocation],
		TimeOfLatestCheckpoint:       pgDataMap[PgControlDataKeyTimeOfLatestCheckpoint],
		OperatorVersion:              versions.Info.Version,
	}

	token, err := json.Marshal(content)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(token), nil
}

// ParsePgControldataToken parses the JSON token into usable content
func ParsePgControldataToken(base64Token string) (*PgControldataTokenContent, error) {
	token, err := base64.StdEncoding.DecodeString(base64Token)
	if err != nil {
		return nil, &ErrInvalidShutdownToken{
			err:    err,
			reason: "Base64 decoding failed",
		}
	}

	var content PgControldataTokenContent
	if err = json.Unmarshal(token, &content); err != nil {
		return nil, &ErrInvalidShutdownToken{
			err:    err,
			reason: "JSON decoding failed",
		}
	}

	return &content, nil
}
