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
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistration(t *testing.T) {
	// Test if metrics are registered correctly by checking if they have a descriptor
	if testutil.ToFloat64(VolumeSnapshotRetryTotal.WithLabelValues("test", "test", "provisioning")) != 0 {
		t.Error("VolumeSnapshotRetryTotal should be initialized to 0")
	}

	if testutil.ToFloat64(VolumeSnapshotRetryByErrorTotal.WithLabelValues("test", "test", "timeout", "provisioning")) != 0 {
		t.Error("VolumeSnapshotRetryByErrorTotal should be initialized to 0")
	}

	if testutil.ToFloat64(VolumeSnapshotFailedTotal.WithLabelValues("test", "test", "provisioning")) != 0 {
		t.Error("VolumeSnapshotFailedTotal should be initialized to 0")
	}
}

func TestRecordRetry(t *testing.T) {
	// Reset metrics before test
	VolumeSnapshotRetryTotal.Reset()

	// Record a retry and verify the counter incremented
	RecordRetry("test-ns", "test-snapshot", PhaseProvisioning)

	if testutil.ToFloat64(VolumeSnapshotRetryTotal.WithLabelValues("test-ns", "test-snapshot", "provisioning")) != 1 {
		t.Error("VolumeSnapshotRetryTotal should be incremented to 1")
	}

	// Record another retry and verify counter incremented again
	RecordRetry("test-ns", "test-snapshot", PhaseProvisioning)

	if testutil.ToFloat64(VolumeSnapshotRetryTotal.WithLabelValues("test-ns", "test-snapshot", "provisioning")) != 2 {
		t.Error("VolumeSnapshotRetryTotal should be incremented to 2")
	}
}

func TestRecordRetryWithError(t *testing.T) {
	// Reset metrics before test
	VolumeSnapshotRetryByErrorTotal.Reset()

	// Record a retry with specific error type
	RecordRetryWithError("test-ns", "test-snapshot", PhaseReady, "context.DeadlineExceeded")

	if testutil.ToFloat64(VolumeSnapshotRetryByErrorTotal.WithLabelValues(
		"test-ns", "test-snapshot", "context.DeadlineExceeded", "ready")) != 1 {
		t.Error("VolumeSnapshotRetryByErrorTotal should be incremented to 1")
	}
}

func TestRecordFailed(t *testing.T) {
	// Reset metrics before test
	VolumeSnapshotFailedTotal.Reset()

	// Record a failed operation
	RecordFailed("test-ns", "test-snapshot", PhaseProvisioning)

	if testutil.ToFloat64(VolumeSnapshotFailedTotal.WithLabelValues("test-ns", "test-snapshot", "provisioning")) != 1 {
		t.Error("VolumeSnapshotFailedTotal should be incremented to 1")
	}
}

func TestRecordRetryDuration(t *testing.T) {
	// Reset metrics before test
	VolumeSnapshotRetryDuration.Reset()

	// Record duration of a retry operation
	RecordRetryDuration("test-ns", "test-snapshot", PhaseProvisioning, 15.5)

	// Note: We can't easily test histogram values with testutil directly,
	// but we can verify the code executes without error
}
