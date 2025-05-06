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
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
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
