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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Segment name parsing and generation", func() {
	It("can generate WAL names", func() {
		tests := []struct {
			segment Segment
			name    string
		}{
			{
				Segment{0, 0, 0},
				"000000000000000000000000",
			},
			{
				Segment{1, 1, 1},
				"000000010000000100000001",
			},
			{
				Segment{10, 10, 10},
				"0000000A0000000A0000000A",
			},
			{
				Segment{17, 17, 17},
				"000000110000001100000011",
			},
			{
				Segment{0, 2, 1},
				"000000000000000200000001",
			},
			{
				Segment{1, 0, 2},
				"000000010000000000000002",
			},
			{
				Segment{2, 1, 0},
				"000000020000000100000000",
			},
		}

		for _, test := range tests {
			Expect(test.segment.Name()).To(Equal(test.name))
		}
	})

	It("can parse WAL names", func() {
		tests := []struct {
			name    string
			result  Segment
			isError bool
		}{
			{
				name:    "000000000000000000000000",
				result:  Segment{0, 0, 0},
				isError: false,
			},
			{
				name:    "000000010000000100000001",
				result:  Segment{1, 1, 1},
				isError: false,
			},
			{
				name:    "0000000A0000000A0000000A",
				result:  Segment{10, 10, 10},
				isError: false,
			},
			{
				name:    "000000000000000200000001",
				result:  Segment{0, 2, 1},
				isError: false,
			},
			{
				name:    "000000010000000000000002",
				result:  Segment{1, 0, 2},
				isError: false,
			},
			{
				name:    "000000020000000100000000",
				result:  Segment{2, 1, 0},
				isError: false,
			},
			{
				name:    "00000001000000000000000A.00000020.backup",
				isError: true,
			},
			{
				name:    "00000001.history",
				isError: true,
			},
			{
				name:    "00000000000000000000000",
				isError: true,
			},
			{
				name:    "0000000000000000000000000",
				isError: true,
			},
			{
				name:    "000000000000X00000000000",
				isError: true,
			},
		}

		for _, test := range tests {
			segment, err := SegmentFromName(test.name)
			Expect(err != nil).To(
				Equal(test.isError),
				"Unexpected error status while parsing %s", test.name)
			if err == nil {
				Expect(segment).To(Equal(test.result))
			}
		}
	})

	It("can generate a segment list (when the XLOG segment size is known)", func() {
		pg92 := 90200
		pg93 := 90300
		defaultWalSize := DefaultWALSegmentSize

		tests := []struct {
			start   Segment
			size    int
			walSize *int64
			version *int
			result  []Segment
		}{
			{
				start:   MustSegmentFromName("0000000100000001000000FD"),
				size:    5,
				walSize: &defaultWalSize,
				version: &pg92,
				result: []Segment{
					MustSegmentFromName("0000000100000001000000FD"),
					MustSegmentFromName("0000000100000001000000FE"),
					MustSegmentFromName("000000010000000200000000"),
					MustSegmentFromName("000000010000000200000001"),
					MustSegmentFromName("000000010000000200000002"),
				},
			},
			{
				start:   MustSegmentFromName("0000000100000001000000FD"),
				size:    5,
				walSize: &defaultWalSize,
				version: &pg93,
				result: []Segment{
					MustSegmentFromName("0000000100000001000000FD"),
					MustSegmentFromName("0000000100000001000000FE"),
					MustSegmentFromName("0000000100000001000000FF"),
					MustSegmentFromName("000000010000000200000000"),
					MustSegmentFromName("000000010000000200000001"),
				},
			},
			{
				start:   MustSegmentFromName("0000000100000001000000FD"),
				size:    2,
				walSize: &defaultWalSize,
				version: &pg92,
				result: []Segment{
					MustSegmentFromName("0000000100000001000000FD"),
					MustSegmentFromName("0000000100000001000000FE"),
				},
			},
		}

		for _, test := range tests {
			Expect(test.start.NextSegments(test.size, test.version, test.walSize)).To(
				Equal(test.result),
				"start=%v size=%v version=%v walSize=%v",
				test.start.Name(), test.size, test.version, test.walSize)
		}
	})
})

var _ = Describe("WAL files checking", func() {
	It("checks whether a file is a WAL file or not by its name", func() {
		tests := []struct {
			name   string
			result bool
		}{
			{
				name:   "000000000000000200000001",
				result: true,
			},
			{
				name:   "test/000000000000000200000001",
				result: true,
			},
			{
				name:   "00000001000000000000000A.00000020.backup",
				result: false,
			},
			{
				name:   "00000002.history",
				result: false,
			},
			{
				name:   "00000000000000000000000",
				result: false,
			},
			{
				name:   "0000000000000000000000000",
				result: false,
			},
			{
				name:   "000000000000X00000000000",
				result: false,
			},
			{
				name:   "00000001000000000000000A.backup",
				result: false,
			},
			{
				name:   "00000001000000000000000A.history",
				result: false,
			},
			{
				name:   "00000001000000000000000A.partial",
				result: false,
			},
		}

		for _, test := range tests {
			Expect(IsWALFile(test.name)).To(
				Equal(test.result), "name:%v expected:%v", test.name, test.result)
		}
	})
})

var _ = Describe("BuildWALPath", func() {
	const pgData = "/var/lib/postgresql/data/pgdata"

	Context("when walPath is a relative path", func() {
		It("should join pgData with the relative path", func() {
			relativePath := "pg_wal/000000010000000000000001"
			expectedPath := "/var/lib/postgresql/data/pgdata/pg_wal/000000010000000000000001"

			result := BuildWALPath(pgData, relativePath)

			Expect(result).To(Equal(expectedPath))
		})

		It("should handle just a filename", func() {
			filename := "000000010000000000000001"
			expectedPath := "/var/lib/postgresql/data/pgdata/000000010000000000000001"

			result := BuildWALPath(pgData, filename)

			Expect(result).To(Equal(expectedPath))
		})

		It("should handle subdirectory with filename", func() {
			relativePath := "pg_wal/archive_status/000000010000000000000001.ready"
			expectedPath := "/var/lib/postgresql/data/pgdata/pg_wal/archive_status/000000010000000000000001.ready"

			result := BuildWALPath(pgData, relativePath)

			Expect(result).To(Equal(expectedPath))
		})
	})

	Context("when walPath is an absolute path", func() {
		It("should use the absolute path as-is without joining with pgData", func() {
			absolutePath := "/var/lib/postgresql/data/pgdata/pg_wal/000000010000000000000001"

			result := BuildWALPath(pgData, absolutePath)

			Expect(result).To(Equal(absolutePath))
		})

		It("should not duplicate pgData when path is already absolute (the bug scenario from #9067)", func() {
			// This is the scenario from issue #9067: pg_rewind passes an absolute path
			absolutePath := "/var/lib/postgresql/data/pgdata/pg_wal/00000001000000010000005F"

			result := BuildWALPath(pgData, absolutePath)

			// Verify the bug is fixed: result should be the absolute path, not duplicated
			Expect(result).To(Equal(absolutePath))
			// Verify it doesn't create the buggy duplicated path
			Expect(result).NotTo(ContainSubstring("/var/lib/postgresql/data/pgdata/var/lib/postgresql/data/pgdata"))
		})

		It("should handle absolute paths from different roots", func() {
			differentAbsolutePath := "/tmp/wal/000000010000000000000001"

			result := BuildWALPath(pgData, differentAbsolutePath)

			Expect(result).To(Equal(differentAbsolutePath))
		})
	})
})

var _ = Describe("Timeline history filename parsing", func() {
	It("can parse timeline from history filenames", func() {
		tests := []struct {
			name             string
			expectedTimeline int
			expectError      bool
		}{
			{
				name:             "00000001.history",
				expectedTimeline: 1,
				expectError:      false,
			},
			{
				name:             "00000021.history", // 0x21 = 33 decimal
				expectedTimeline: 33,
				expectError:      false,
			},
			{
				name:             "0000002A.history", // 0x2A = 42 decimal
				expectedTimeline: 42,
				expectError:      false,
			},
			{
				name:             "000000FF.history", // 0xFF = 255 decimal
				expectedTimeline: 255,
				expectError:      false,
			},
			{
				name:             "0000000A.history", // 0x0A = 10 decimal
				expectedTimeline: 10,
				expectError:      false,
			},
			{
				name:             "/var/lib/postgresql/00000021.history", // with path
				expectedTimeline: 33,
				expectError:      false,
			},
			// Error cases
			{
				name:        "00000001", // missing .history extension
				expectError: true,
			},
			{
				name:        "0000001.history", // wrong length (7 digits instead of 8)
				expectError: true,
			},
			{
				name:        "000000001.history", // wrong length (9 digits instead of 8)
				expectError: true,
			},
			{
				name:        "0000000X.history", // invalid hex character
				expectError: true,
			},
			{
				name:        "000000010000000000000001", // regular WAL file, not history
				expectError: true,
			},
			{
				name:        ".history", // no timeline digits
				expectError: true,
			},
		}

		for _, test := range tests {
			timeline, err := ParseTimelineFromHistoryFilename(test.name)
			if test.expectError {
				Expect(err).To(HaveOccurred(), "Expected error for name: %s", test.name)
			} else {
				Expect(err).NotTo(HaveOccurred(), "Unexpected error for name: %s", test.name)
				Expect(timeline).To(Equal(test.expectedTimeline), "Timeline mismatch for name: %s", test.name)
			}
		}
	})
})

var _ = Describe("Timeline history filename generation", func() {
	It("builds the expected 8-hex-digit history filename", func() {
		Expect(TimelineHistoryFileName(1)).To(Equal("00000001.history"))
		Expect(TimelineHistoryFileName(33)).To(Equal("00000021.history"))
		Expect(TimelineHistoryFileName(255)).To(Equal("000000FF.history"))
	})

	It("round-trips with ParseTimelineFromHistoryFilename", func() {
		for _, tli := range []int{1, 2, 10, 33, 255, 4096} {
			timeline, err := ParseTimelineFromHistoryFilename(TimelineHistoryFileName(tli))
			Expect(err).NotTo(HaveOccurred())
			Expect(timeline).To(Equal(tli))
		}
	})
})

var _ = Describe("Timeline fork point lookup", func() {
	It("finds the fork LSN for the requested parent timeline", func() {
		content := "1\t0/7000110\tno recovery target specified\n"
		lsn, found := FindTimelineForkPoint(content, 1)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/7000110"))
	})

	It("finds the right fork point among several ancestor timelines", func() {
		// timeline 2 forked from 1 at 0/5000000, and the current timeline
		// (3) forked from 2 at 0/8000000. A replica still on timeline 1
		// forked away at 0/5000000, not at 0/8000000.
		content := "1\t0/5000000\tno recovery target specified\n" +
			"2\t0/8000000\tno recovery target specified\n"

		lsn, found := FindTimelineForkPoint(content, 1)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/5000000"))

		lsn, found = FindTimelineForkPoint(content, 2)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/8000000"))
	})

	It("tolerates comments, blank lines, and space-separated fields", func() {
		content := "# comment\n\n1 0/7000110 no recovery target specified\n\n"
		lsn, found := FindTimelineForkPoint(content, 1)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/7000110"))
	})

	It("returns false when the parent timeline is not in the history", func() {
		content := "1\t0/7000110\tno recovery target specified\n"
		_, found := FindTimelineForkPoint(content, 5)
		Expect(found).To(BeFalse())
	})

	It("returns false on empty content", func() {
		_, found := FindTimelineForkPoint("", 1)
		Expect(found).To(BeFalse())
	})

	It("ignores malformed lines instead of erroring", func() {
		content := "not-a-number\t0/7000110\tbad line\n" +
			"1\t0/7000110\tno recovery target specified\n"
		lsn, found := FindTimelineForkPoint(content, 1)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/7000110"))
	})

	It("returns the first match when a timeline appears more than once", func() {
		// Not expected in a real PostgreSQL history file, but the parser
		// should not depend on that assumption to behave predictably.
		content := "1\t0/5000000\tfirst\n1\t0/9000000\tsecond\n"
		lsn, found := FindTimelineForkPoint(content, 1)
		Expect(found).To(BeTrue())
		Expect(lsn).To(BeEquivalentTo("0/5000000"))
	})
})
