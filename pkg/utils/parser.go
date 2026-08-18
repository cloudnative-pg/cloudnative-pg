/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

type pgControlDataKey = string

const (
	// pgControlDataKeyLatestCheckpointTimelineID is the
	// latest checkpoint's TimeLineID pg_controldata entry
	pgControlDataKeyLatestCheckpointTimelineID pgControlDataKey = "Latest checkpoint's TimeLineID"

	// pgControlDataKeyREDOWALFile is the latest checkpoint's
	// REDO WAL file pg_controldata entry
	pgControlDataKeyREDOWALFile pgControlDataKey = "Latest checkpoint's REDO WAL file"

	// pgControlDataKeyDatabaseSystemIdentifier is the database
	// system identifier pg_controldata entry
	pgControlDataKeyDatabaseSystemIdentifier pgControlDataKey = "Database system identifier"

	// pgControlDataKeyLatestCheckpointREDOLocation is the latest
	// checkpoint's REDO location pg_controldata entry
	pgControlDataKeyLatestCheckpointREDOLocation pgControlDataKey = "Latest checkpoint's REDO location"

	// pgControlDataKeyTimeOfLatestCheckpoint is the time
	// of latest checkpoint pg_controldata entry
	pgControlDataKeyTimeOfLatestCheckpoint pgControlDataKey = "Time of latest checkpoint"

	// pgControlDataDatabaseClusterStateKey is the status
	// of the latest primary that run on this data directory.
	pgControlDataDatabaseClusterStateKey pgControlDataKey = "Database cluster state"

	// pgControlDataDataPageChecksumVersion reports whether the checksums are enabled in the cluster
	pgControlDataDataPageChecksumVersion pgControlDataKey = "Data page checksum version"

	// pgControlDataBytesPerWALSegment reports the size of the WAL segments
	pgControlDataBytesPerWALSegment pgControlDataKey = "Bytes per WAL segment"
)

// PgControlData represents the parsed output of pg_controldata
type PgControlData map[pgControlDataKey]string

// GetLatestCheckpointTimelineID returns the latest checkpoint's TimeLineID
func (p PgControlData) GetLatestCheckpointTimelineID() string {
	return p[pgControlDataKeyLatestCheckpointTimelineID]
}

// TryGetLatestCheckpointTimelineID returns the latest checkpoint's TimeLineID
func (p PgControlData) TryGetLatestCheckpointTimelineID() (string, bool) {
	v, ok := p[pgControlDataKeyLatestCheckpointTimelineID]
	return v, ok
}

// GetREDOWALFile returns the latest checkpoint's REDO WAL file
func (p PgControlData) GetREDOWALFile() string {
	return p[pgControlDataKeyREDOWALFile]
}

// TryGetREDOWALFile returns the latest checkpoint's REDO WAL file
func (p PgControlData) TryGetREDOWALFile() (string, bool) {
	v, ok := p[pgControlDataKeyREDOWALFile]
	return v, ok
}

// GetDatabaseSystemIdentifier returns the database system identifier
func (p PgControlData) GetDatabaseSystemIdentifier() string {
	return p[pgControlDataKeyDatabaseSystemIdentifier]
}

// GetLatestCheckpointREDOLocation returns the latest checkpoint's REDO location
func (p PgControlData) GetLatestCheckpointREDOLocation() string {
	return p[pgControlDataKeyLatestCheckpointREDOLocation]
}

// GetTimeOfLatestCheckpoint returns the time of latest checkpoint
func (p PgControlData) GetTimeOfLatestCheckpoint() string {
	return p[pgControlDataKeyTimeOfLatestCheckpoint]
}

// GetDatabaseClusterState returns the status of the latest primary that ran on this data directory
func (p PgControlData) GetDatabaseClusterState() string {
	return p[pgControlDataDatabaseClusterStateKey]
}

// GetDataPageChecksumVersion returns whether the checksums are enabled in the cluster
func (p PgControlData) GetDataPageChecksumVersion() (string, error) {
	value, ok := p[pgControlDataDataPageChecksumVersion]
	if !ok {
		return "", fmt.Errorf("no '%s' section in pg_controldata output", pgControlDataDataPageChecksumVersion)
	}
	return value, nil
}

// GetBytesPerWALSegment returns the size of the WAL segments
func (p PgControlData) GetBytesPerWALSegment() (int, error) {
	value, ok := p[pgControlDataBytesPerWALSegment]
	if !ok {
		return 0, fmt.Errorf("no '%s' section in pg_controldata output", pgControlDataBytesPerWALSegment)
	}

	walSegmentSize, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf(
			"wrong '%s' pg_controldata value (not an integer): '%s' %w",
			pgControlDataBytesPerWALSegment, value, err)
	}

	return walSegmentSize, nil
}

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
func ParsePgControldataOutput(data string) PgControlData {
	pairs := make(map[string]string)
	lines := strings.SplitSeq(data, "\n")
	for line := range lines {
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
func (p PgControlData) CreatePromotionToken() (string, error) {
	content := PgControldataTokenContent{
		LatestCheckpointTimelineID:   p.GetLatestCheckpointTimelineID(),
		REDOWALFile:                  p.GetREDOWALFile(),
		DatabaseSystemIdentifier:     p.GetDatabaseSystemIdentifier(),
		LatestCheckpointREDOLocation: p.GetLatestCheckpointREDOLocation(),
		TimeOfLatestCheckpoint:       p.GetTimeOfLatestCheckpoint(),
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
