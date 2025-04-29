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

package promotiontoken

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/postgres/controldata"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// Data contains the data needed to properly create a promotion token
type Data struct {
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
func (token *Data) IsValid() error {
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
func (token *Data) Encode() (string, error) {
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

// FromControlDataInfo translates a parsed pgControlData into a JSON token
func FromControlDataInfo(p controldata.Info) (string, error) {
	content := Data{
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

// Parse parses the JSON token into usable content
func Parse(base64Token string) (*Data, error) {
	token, err := base64.StdEncoding.DecodeString(base64Token)
	if err != nil {
		return nil, &ErrInvalidPromotionToken{
			err:    err,
			reason: "Base64 decoding failed",
		}
	}

	var content Data
	if err = json.Unmarshal(token, &content); err != nil {
		return nil, &ErrInvalidPromotionToken{
			err:    err,
			reason: "JSON decoding failed",
		}
	}

	return &content, nil
}
