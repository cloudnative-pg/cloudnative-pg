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

	// identity is the instance doing the IsCompleted check in every test.
	identity := MarkerIdentity{
		Namespace:   "default",
		ClusterName: "cluster-example",
		ClusterUID:  "11111111-1111-1111-1111-111111111111",
		PodName:     "cluster-example-1",
	}

	BeforeEach(func() {
		pgData = GinkgoT().TempDir()
	})

	It("reports not completed on a fresh data directory", func() {
		completed, err := IsCompleted(pgData, identity)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("writes a marker that the same instance then detects, and is parseable", func() {
		Expect(WriteCompletedMarker(pgData, ModeInitDB, identity)).To(Succeed())

		completed, err := IsCompleted(pgData, identity)
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
		Expect(marker.Identity).To(Equal(identity))
	})

	It("still reports not completed when only unrelated files exist", func() {
		Expect(os.WriteFile(filepath.Join(pgData, "PG_VERSION"), []byte("17"), 0o600)).To(Succeed())

		completed, err := IsCompleted(pgData, identity)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	// A marker written by a different instance is exactly what a snapshot-cloned
	// PVC carries: the check must not treat it as this instance's completion.
	DescribeTable("reports not completed when the marker belongs to another instance",
		func(author MarkerIdentity) {
			Expect(WriteCompletedMarker(pgData, ModeRestoreSnapshot, author)).To(Succeed())

			completed, err := IsCompleted(pgData, identity)
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeFalse())
		},
		Entry("different pod name (snapshot-cloned scale-up replica)", MarkerIdentity{
			Namespace:   identity.Namespace,
			ClusterName: identity.ClusterName,
			ClusterUID:  identity.ClusterUID,
			PodName:     "cluster-example-2",
		}),
		Entry("different cluster UID, same name and namespace (in-place recreation)", MarkerIdentity{
			Namespace:   identity.Namespace,
			ClusterName: identity.ClusterName,
			ClusterUID:  "22222222-2222-2222-2222-222222222222",
			PodName:     identity.PodName,
		}),
		Entry("different namespace", MarkerIdentity{
			Namespace:   "other",
			ClusterName: identity.ClusterName,
			ClusterUID:  identity.ClusterUID,
			PodName:     identity.PodName,
		}),
	)

	It("reports not completed for an old-format marker without identity fields", func() {
		// A marker produced before identity scoping parses cleanly but leaves the
		// identity zero-valued, which cannot match a real instance.
		legacy := []byte(`{"mode":"restoresnapshot","completedAt":"2020-01-01T00:00:00Z","operatorVersion":"1.0"}`)
		Expect(os.WriteFile(filepath.Join(pgData, constants.BootstrapCompletedFile), legacy, 0o600)).To(Succeed())

		completed, err := IsCompleted(pgData, identity)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("reports not completed, without error, for a corrupt marker", func() {
		Expect(os.WriteFile(
			filepath.Join(pgData, constants.BootstrapCompletedFile),
			[]byte("this is not json"), 0o600),
		).To(Succeed())

		completed, err := IsCompleted(pgData, identity)
		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})
})
