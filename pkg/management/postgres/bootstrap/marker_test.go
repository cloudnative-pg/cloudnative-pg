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

package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bootstrap completion marker", func() {
	var pgData string

	BeforeEach(func() {
		pgData = GinkgoT().TempDir()
	})

	It("reports not completed on a fresh data directory", func() {
		completed, err := IsCompleted(pgData)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("writes a marker that is then detected and parseable", func() {
		Expect(WriteCompletedMarker(pgData, ModeInitDB)).To(Succeed())

		completed, err := IsCompleted(pgData)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeTrue())

		// #nosec G304 -- pgData is a test-controlled temporary directory
		content, err := os.ReadFile(filepath.Join(pgData, constants.BootstrapCompletedFile))
		Expect(err).ToNot(HaveOccurred())

		var marker completionMarker
		Expect(json.Unmarshal(content, &marker)).To(Succeed())
		Expect(marker.Mode).To(Equal(string(ModeInitDB)))
		Expect(marker.OperatorVersion).To(Equal(versions.Version))
		Expect(marker.CompletedAt).ToNot(BeZero())
	})

	It("still reports not completed when only unrelated files exist", func() {
		Expect(os.WriteFile(filepath.Join(pgData, "PG_VERSION"), []byte("17"), 0o600)).To(Succeed())

		completed, err := IsCompleted(pgData)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})
})
