/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package postgres

import (
	"errors"
	"fmt"
	"path"
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
	return int32(0xFFFFFFFF / walSegmentSize)
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
