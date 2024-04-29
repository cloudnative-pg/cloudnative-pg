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
	"strings"
)

type pgControlDataKey = string

// A key of pg_controldata
const (
	PgControlDataKeyLatestCheckpointTimelineID   pgControlDataKey = "Latest checkpoint's TimeLineID"
	PgControlDataKeyREDOWALFile                  pgControlDataKey = "Latest checkpoint's REDO WAL file"
	PgControlDataKeyDatabaseSystemIdentifier     pgControlDataKey = "Database system identifier"
	PgControlDataKeyLatestCheckpointREDOLocation pgControlDataKey = "Latest checkpoint's REDO location"
	PgControlDataKeyTimeOfLatestCheckpoint       pgControlDataKey = "Time of latest checkpoint"
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
	LatestCheckpointTimelineID   string `json:"latestCheckpointTimelineID,omitempty"`
	REDOWALFile                  string `json:"redoWalFile,omitempty"`
	DatabaseSystemIdentifier     string `json:"databaseSystemIdentifier,omitempty"`
	LatestCheckpointREDOLocation string `json:"latestCheckpointREDOLocation,omitempty"`
	TimeOfLatestCheckpoint       string `json:"timeOfLatestCheckpoint,omitempty"`
}

// CreatePgControldataToken translates a parsed pgControlData into a JSON token
func CreatePgControldataToken(data map[string]string) (string, error) {
	content := PgControldataTokenContent{
		LatestCheckpointTimelineID:   data[PgControlDataKeyLatestCheckpointTimelineID],
		REDOWALFile:                  data[PgControlDataKeyREDOWALFile],
		DatabaseSystemIdentifier:     data[PgControlDataKeyDatabaseSystemIdentifier],
		LatestCheckpointREDOLocation: data[PgControlDataKeyLatestCheckpointREDOLocation],
		TimeOfLatestCheckpoint:       data[PgControlDataKeyTimeOfLatestCheckpoint],
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
		return nil, err
	}
	var content PgControldataTokenContent
	err = json.Unmarshal(token, &content)
	return &content, err
}
