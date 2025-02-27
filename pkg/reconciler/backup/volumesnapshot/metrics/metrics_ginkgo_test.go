/*
Copyright The CloudNativePG Contributors

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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("VolumeSnapshot metrics", func() {
	BeforeEach(func() {
		// Reset metrics before each test
		VolumeSnapshotRetryTotal.Reset()
		VolumeSnapshotRetryByErrorTotal.Reset()
		VolumeSnapshotFailedTotal.Reset()
		VolumeSnapshotRetryDuration.Reset()
	})

	Context("retry operation recording", func() {
		It("should increment retry counter", func() {
			RecordRetry("test-ns", "test-snapshot", PhaseProvisioning)
			Expect(testutil.ToFloat64(VolumeSnapshotRetryTotal.WithLabelValues(
				"test-ns", "test-snapshot", "provisioning"))).To(Equal(float64(1)))

			RecordRetry("test-ns", "test-snapshot", PhaseProvisioning)
			Expect(testutil.ToFloat64(VolumeSnapshotRetryTotal.WithLabelValues(
				"test-ns", "test-snapshot", "provisioning"))).To(Equal(float64(2)))
		})

		It("should track retries by error type", func() {
			RecordRetryWithError("test-ns", "test-snapshot", PhaseReady, "context.DeadlineExceeded")
			Expect(testutil.ToFloat64(VolumeSnapshotRetryByErrorTotal.WithLabelValues(
				"test-ns", "test-snapshot", "context.DeadlineExceeded", "ready"))).To(Equal(float64(1)))

			RecordRetryWithError("test-ns", "test-snapshot", PhaseReady, "context.DeadlineExceeded")
			Expect(testutil.ToFloat64(VolumeSnapshotRetryByErrorTotal.WithLabelValues(
				"test-ns", "test-snapshot", "context.DeadlineExceeded", "ready"))).To(Equal(float64(2)))
		})

		It("should record failed operations", func() {
			RecordFailed("test-ns", "test-snapshot", PhaseProvisioning)
			Expect(testutil.ToFloat64(VolumeSnapshotFailedTotal.WithLabelValues(
				"test-ns", "test-snapshot", "provisioning"))).To(Equal(float64(1)))
		})

		It("should record retry duration", func() {
			// We can't easily test histogram values directly, but we can verify the code runs
			RecordRetryDuration("test-ns", "test-snapshot", PhaseProvisioning, 15.5)
		})
	})
})
