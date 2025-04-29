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

	"github.com/cloudnative-pg/machinery/pkg/postgres/controldata"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const fakeControlData = `pg_control version number:               1002
Catalog version number:                  202201241
Database cluster state:                  shut down
Database system identifier:              12345678901234567890123456789012
Latest checkpoint's TimeLineID:       3
pg_control last modified:                2024-04-30 12:00:00 UTC
Latest checkpoint location:              0/3000FF0
Prior checkpoint location:               0/2000AA0
Minimum recovery ending location:        0/3000000
Time of latest checkpoint:               2024-04-30 10:00:00 UTC
Database block size:                     8192 bytes
Latest checkpoint's REDO location:         0/3000CC0
Latest checkpoint's REDO WAL file:         000000010000000000000003
Blocks per segment of large relation:    131072
Maximum data alignment:                  8
Database disk usage:                     10240 KB
Maximum xlog ID:                         123456789
Next xlog byte position:                 0/3000010`

var _ = Describe("promotion token creation", func() {
	It("creates a promotion token from a parsed pg_controldata", func() {
		parsedControlData := controldata.ParseOutput(fakeControlData)

		decodeBase64 := func(s string) error {
			_, err := base64.StdEncoding.DecodeString(s)
			return err
		}

		token, err := FromControlDataInfo(parsedControlData)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).ToNot(BeEmpty())
		Expect(decodeBase64(token)).To(Succeed())
	})
})

var _ = Describe("promotion token parser", func() {
	It("parses a newly generated promotion token", func() {
		parsedControlData := controldata.ParseOutput(fakeControlData)

		token, err := FromControlDataInfo(parsedControlData)
		Expect(err).ToNot(HaveOccurred())

		tokenContent, err := Parse(token)
		Expect(err).ToNot(HaveOccurred())
		Expect(tokenContent).ToNot(BeNil())
		Expect(*tokenContent).To(Equal(Data{
			LatestCheckpointTimelineID:   "3",
			REDOWALFile:                  "000000010000000000000003",
			DatabaseSystemIdentifier:     "12345678901234567890123456789012",
			LatestCheckpointREDOLocation: "0/3000CC0",
			TimeOfLatestCheckpoint:       "2024-04-30 10:00:00 UTC",
			OperatorVersion:              versions.Info.Version,
		}))
	})

	It("fails when the promotion token is not encoded in base64", func() {
		tokenContent, err := Parse("***(((((((|||||||||)))))))")
		Expect(err).To(HaveOccurred())
		Expect(tokenContent).To(BeNil())
	})

	It("fails when the JSON content of the base64 token is not correct", func() {
		jsonContent := `{"test`
		encodedToken := base64.StdEncoding.EncodeToString([]byte(jsonContent))
		tokenContent, err := Parse(encodedToken)
		Expect(err).To(HaveOccurred())
		Expect(tokenContent).To(BeNil())
	})
})

var _ = Describe("promotion token validation", func() {
	It("validates a newly generated promotion token", func() {
		parsedControlData := controldata.ParseOutput(fakeControlData)

		token, err := FromControlDataInfo(parsedControlData)
		Expect(err).ToNot(HaveOccurred())

		tokenContent, err := Parse(token)
		Expect(err).ToNot(HaveOccurred())

		err = tokenContent.IsValid()
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails to validate an incorrect token", func() {
		token := Data{
			LatestCheckpointTimelineID: "3",
			// REDOWALFile is missing
			DatabaseSystemIdentifier:     "12345678901234567890123456789012",
			LatestCheckpointREDOLocation: "0/3000CC0",
			TimeOfLatestCheckpoint:       "2024-04-30 10:00:00 UTC",
			OperatorVersion:              versions.Info.Version,
		}

		err := token.IsValid()
		Expect(err).To(HaveOccurred())
	})
})
