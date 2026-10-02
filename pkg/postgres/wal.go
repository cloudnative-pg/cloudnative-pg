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

package postgres

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
)

const (
	// DefaultWALSegmentSize is the default size of a single WAL file
	// This must be a power of 2
	DefaultWALSegmentSize = int64(1 << 24)

	// WALHexOctetRe is a regex to match 8 Hex characters
	WALHexOctetRe = `([\dA-Fa-f]{8})`

	// WALTimeLineRe is a regex to match the timeline in a WAL filename
	WALTimeLineRe = WALHexOctetRe

	// WALSegmentNameRe is a regex to match the segment parent log file and segment id
	WALSegmentNameRe = WALHexOctetRe + WALHexOctetRe
)

var (
	// WALRe is the file segment name parser
	WALRe = regexp.MustCompile(`^` +
		// everything has a timeline
		WALTimeLineRe +
		// (1) optional
		`(?:` +
		// segment name, if a wal file
		WALSegmentNameRe +
		// and (2) optional
		`(?:` +
		// offset, if a backup label
		`\.[\dA-Fa-f]{8}\.backup` +
		// or
		`|` +
		// partial, if a partial file
		`\.partial` +
		// close (2)
		`)?` +
		// or
		`|` +
		// only .history, if a history file
		`\.history` +
		// close (1)
		`)$`)

	// WALSegmentRe is the file segment name parser
	WALSegmentRe = regexp.MustCompile(`^` +
		// everything has a timeline
		WALTimeLineRe +
		// segment name, if a wal file
		WALSegmentNameRe +
		`$`)

	// ErrorBadWALSegmentName is raised when parsing an invalid segment name
	ErrorBadWALSegmentName = errors.New("invalid WAL segment name")

	// ErrorBadTimelineHistoryName is raised when parsing an invalid timeline history filename
	ErrorBadTimelineHistoryName = errors.New("invalid timeline history filename")
)

// Segment contains the information inside a WAL segment name
type Segment struct {
	// Timeline number
	Tli int32

	// Log number
	Log int32

	// Segment number
	Seg int32
}

// IsWALFile check if the passed file name is a regular WAL file.
// It supports either a full file path or a simple file name
func IsWALFile(name string) bool {
	baseName := path.Base(name)
	return WALSegmentRe.MatchString(baseName)
}

// ParseTimelineFromHistoryFilename extracts the timeline ID from a timeline history filename.
// For example, "00000021.history" returns 33 (0x21 in hex).
func ParseTimelineFromHistoryFilename(name string) (int, error) {
	baseName := path.Base(name)

	// Timeline history files are exactly 16 characters: 8 hex digits + ".history" (8 chars)
	if len(baseName) < 16 || baseName[8:] != ".history" {
		return 0, ErrorBadTimelineHistoryName
	}

	timelineHex := baseName[0:8]
	timeline, err := strconv.ParseInt(timelineHex, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrorBadTimelineHistoryName, err)
	}

	return int(timeline), nil
}

// SegmentFromName retrieves the timeline, log ID and segment ID
// from the name of a xlog segment, and can also handle a full path
// or a simple file name
func SegmentFromName(name string) (Segment, error) {
	var tli, log, seg int64
	var err error

	baseName := path.Base(name)
	// We could have used WALSegmentRe directly, but we wanted to adhere to barman code
	subMatches := WALRe.FindStringSubmatch(baseName)
	if len(subMatches) != 4 {
		return Segment{}, ErrorBadWALSegmentName
	}

	if len(subMatches[0]) != 24 {
		return Segment{}, ErrorBadWALSegmentName
	}

	if tli, err = strconv.ParseInt(subMatches[1], 16, 32); err != nil {
		return Segment{}, ErrorBadWALSegmentName
	}

	if log, err = strconv.ParseInt(subMatches[2], 16, 32); err != nil {
		return Segment{}, ErrorBadWALSegmentName
	}

	if seg, err = strconv.ParseInt(subMatches[3], 16, 32); err != nil {
		return Segment{}, ErrorBadWALSegmentName
	}

	return Segment{
		Tli: int32(tli),
		Log: int32(log),
		Seg: int32(seg),
	}, nil
}

// MustSegmentFromName is analogous to SegmentFromName but panics
// if the segment name is invalid
func MustSegmentFromName(name string) Segment {
	result, err := SegmentFromName(name)
	if err != nil {
		panic(err)
	}

	return result
}

// Name gets the name of the segment
func (segment Segment) Name() string {
	return fmt.Sprintf("%08X%08X%08X", segment.Tli, segment.Log, segment.Seg)
}

// WalSegmentsPerFile is the number of WAL Segments in a WAL File
func WalSegmentsPerFile(walSegmentSize int64) int32 {
	// Given that segment section is represented by 8 hex characters,
	// we compute the number of wal segments in a file, by dividing
	// the "max segment number" by the wal segment size.
	return int32(0xFFFFFFFF / walSegmentSize) //nolint:gosec
}

// NextSegments generate the list of all possible segment names starting
// from `segment`, until the specified size is reached. This function will
// not ever generate timeline changes.
// If postgresVersion == nil, the latest postgres version is assumed.
// If segmentSize == nil, wal_segment_size=DefaultWALSegmentSize is assumed.
func (segment Segment) NextSegments(size int, postgresVersion *int, segmentSize *int64) []Segment {
	result := make([]Segment, 0, size)

	var walSegPerFile int32
	if segmentSize == nil {
		walSegPerFile = WalSegmentsPerFile(DefaultWALSegmentSize)
	} else {
		walSegPerFile = WalSegmentsPerFile(*segmentSize)
	}

	skipLastSegment := postgresVersion != nil && *postgresVersion < 90300

	currentSegment := segment
	for len(result) < size {
		result = append(result, Segment{
			Tli: currentSegment.Tli,
			Log: currentSegment.Log,
			Seg: currentSegment.Seg,
		})
		currentSegment.Seg++
		if currentSegment.Seg > walSegPerFile || (skipLastSegment && currentSegment.Seg == walSegPerFile) {
			currentSegment.Log++
			currentSegment.Seg = 0
		}
	}

	return result
}

// BuildWALPath constructs the full destination path for WAL operations.
// If walPath is already absolute, it returns it as-is.
// If walPath is relative, it joins it with pgData.
// This prevents path duplication when PostgreSQL or pg_rewind passes absolute paths.
func BuildWALPath(pgData, walPath string) string {
	if !filepath.IsAbs(walPath) {
		return filepath.Join(pgData, walPath)
	}
	return walPath
}
