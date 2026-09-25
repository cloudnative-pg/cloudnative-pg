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

package archiver

import (
	"context"
	"os"
	"path/filepath"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// skipEmptyWalArchiveCheckAnnotation mirrors the unexported constant in
// pkg/utils; the tests deliberately hard-code the literal so a divergence
// between the annotation the operator honors and the one documented here
// would surface as a failure.
const skipEmptyWalArchiveCheckAnnotation = "cnpg.io/skipEmptyWalArchiveCheck"

func writeMarkerFile(pgData string) {
	markerPath := filepath.Join(pgData, constants.CheckEmptyWalArchiveFile)
	Expect(os.WriteFile(markerPath, []byte{}, 0o600)).To(Succeed())
}

var _ = Describe("shouldCheckEmptyWalArchive", func() {
	DescribeTable(
		"combines the skip annotation with the first-archive marker file",
		func(annotationValue *string, createMarker bool, expected bool) {
			pgData := GinkgoT().TempDir()
			if createMarker {
				writeMarkerFile(pgData)
			}

			cluster := &apiv1.Cluster{}
			if annotationValue != nil {
				cluster.Annotations = map[string]string{
					skipEmptyWalArchiveCheckAnnotation: *annotationValue,
				}
			}

			Expect(shouldCheckEmptyWalArchive(context.Background(), cluster, pgData)).To(Equal(expected))
		},
		Entry("no annotation and marker present: check runs", nil, true, true),
		Entry("no annotation and marker absent: check skipped", nil, false, false),
		Entry("opt-out annotation and marker present: check skipped", ptr.To("enabled"), true, false),
		Entry("opt-out annotation and marker absent: check skipped", ptr.To("enabled"), false, false),
		Entry("unrelated annotation value and marker present: check runs", ptr.To("something-else"), true, true),
		Entry("unrelated annotation value and marker absent: check skipped", ptr.To("something-else"), false, false),
		Entry("empty annotation value and marker present: check runs", ptr.To(""), true, true),
	)
})

var _ = Describe("isCheckWalArchiveFlagFilePresent", func() {
	It("returns true when the marker file exists in PGDATA", func() {
		pgData := GinkgoT().TempDir()
		writeMarkerFile(pgData)

		Expect(isCheckWalArchiveFlagFilePresent(context.Background(), pgData)).To(BeTrue())
	})

	It("returns false when the marker file is absent", func() {
		pgData := GinkgoT().TempDir()

		Expect(isCheckWalArchiveFlagFilePresent(context.Background(), pgData)).To(BeFalse())
	})

	It("returns false when only an unrelated file is present", func() {
		pgData := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(pgData, "PG_VERSION"), []byte{}, 0o600)).To(Succeed())

		Expect(isCheckWalArchiveFlagFilePresent(context.Background(), pgData)).To(BeFalse())
	})
})
