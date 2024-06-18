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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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

	// PgControlDataDatabaseClusterStateKey is the status
	// of the latest primary that run on this data directory.
	PgControlDataDatabaseClusterStateKey pgControlDataKey = "Database cluster state"
)

// PgDataState represents the "Database cluster state" field of pg_controldata
type PgDataState string

// IsShutdown checks if the PGDATA status represents
// a shut down instance
func (state PgDataState) IsShutdown(ctx context.Context) bool {
	contextLogger := log.FromContext(ctx)

	switch state {
	case "shut down", "shut down in recovery":
		return true

	case "starting up", "shutting down", "in crash recovery", "in archive recovery", "in production":
		return false
	}

	err := fmt.Errorf("unknown pg_controldata cluster state")
	contextLogger.Error(err, "Unknown pg_controldata cluster state, defaulting to running cluster",
		"state", state)
	return false
}

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

// TODO(leonardoce): I believe that the code about the promotion token
// belongs to a different package

// PgControldataTokenContent contains the data needed to properly create a promotion token
type PgControldataTokenContent struct {
	// Latest checkpoint's TimeLineID
	// TODO(leonardoce): should this be an integer?
	LatestCheckpointTimelineID string `json:"latestCheckpointTimelineID,omitempty"`

	// Latest checkpoint's REDO WAL file
	REDOWALFile string `json:"redoWalFile,omitempty"`

	// Database system identifier
	DatabaseSystemIdentifier string `json:"databaseSystemIdentifier,omitempty"`

	// Latest checkpoint's REDO location
	LatestCheckpointREDOLocation string `json:"latestCheckpointREDOLocation,omitempty"`

	// Time of latest checkpoint
	TimeOfLatestCheckpoint string `json:"timeOfLatestCheckpoint,omitempty"`

	// TODO(leonardoce): add a token API version
	// if the token API version is different, the webhook should
	// block the operation

	// The version of the operator that created the token
	// TODO(leonardoce): if the version of the operator is different,
	// the webhook should raise a warning
	OperatorVersion string `json:"operatorVersion,omitempty"`
}

// IsValid checks if the promotion token is valid or
// returns an error otherwise
func (token *PgControldataTokenContent) IsValid() error {
	if len(token.LatestCheckpointTimelineID) == 0 {
		return ErrEmptyLatestCheckpointTimelineID
	}

	if len(token.REDOWALFile) == 0 {
		return ErrEmptyREDOWALFile
	}

	if len(token.DatabaseSystemIdentifier) == 0 {
		return ErrEmptyDatabaseSystemIdentifier
	}

	if len(token.LatestCheckpointREDOLocation) == 0 {
		return ErrEmptyLatestCheckpointREDOLocation
	}

	if len(token.TimeOfLatestCheckpoint) == 0 {
		return ErrEmptyTimeOfLatestCheckpoint
	}

	if len(token.OperatorVersion) == 0 {
		return ErrEmptyOperatorVersion
	}

	return nil
}

// Encode encodes the token content into a base64 string
func (token *PgControldataTokenContent) Encode() (string, error) {
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(tokenJSON), nil
}

// ErrInvalidPromotionToken is raised when the promotion token
// is not valid
type ErrInvalidPromotionToken struct {
	err    error
	reason string
}

func (e *ErrInvalidPromotionToken) Error() string {
	message := fmt.Sprintf("invalid promotion token (%s)", e.reason)
	if e.err != nil {
		message = fmt.Sprintf("%s: %s", message, e.err.Error())
	}
	return message
}

func (e *ErrInvalidPromotionToken) Unwrap() error {
	return e.err
}

var (
	// ErrEmptyLatestCheckpointTimelineID is raised when the relative field
	// in the promotion token is empty
	ErrEmptyLatestCheckpointTimelineID = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "LatestCheckpointTimelineID is empty",
	}

	// ErrEmptyREDOWALFile is raised when the relative field
	// in the promotion token is empty
	ErrEmptyREDOWALFile = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "REDOWALFile is empty",
	}

	// ErrEmptyDatabaseSystemIdentifier is raised when the relative field
	// in the promotion token is empty
	ErrEmptyDatabaseSystemIdentifier = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "DatabaseSystemIdentifier is empty",
	}

	// ErrEmptyLatestCheckpointREDOLocation is raised when the relative field
	// in the promotion token is empty
	ErrEmptyLatestCheckpointREDOLocation = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "LatestCheckpointREDOLocation is empty",
	}

	// ErrEmptyTimeOfLatestCheckpoint is raised when the relative field
	// in the promotion token is empty
	ErrEmptyTimeOfLatestCheckpoint = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "TimeOfLatestCheckpoint is empty",
	}

	// ErrEmptyOperatorVersion is raised when the relative field
	// in the promotion token is empty
	ErrEmptyOperatorVersion = &ErrInvalidPromotionToken{
		err:    nil,
		reason: "OperatorVersion is empty",
	}
)

// CreatePromotionToken translates a parsed pgControlData into a JSON token
func CreatePromotionToken(pgDataMap map[string]string) (string, error) {
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
		return nil, &ErrInvalidPromotionToken{
			err:    err,
			reason: "Base64 decoding failed",
		}
	}

	var content PgControldataTokenContent
	if err = json.Unmarshal(token, &content); err != nil {
		return nil, &ErrInvalidPromotionToken{
			err:    err,
			reason: "JSON decoding failed",
		}
	}

	return &content, nil
}
