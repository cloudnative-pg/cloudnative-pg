/*
Copyright 2019-2022 The CloudNativePG Contributors

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
