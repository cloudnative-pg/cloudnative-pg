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

package autoresize

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HasBudget", func() {
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				AutoResizeEvents: []apiv1.AutoResizeEvent{},
			},
		}
	})

	Context("empty events list", func() {
		It("should return true when no events exist", func() {
			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeTrue(),
				"empty events list should have budget available")
		})
	})

	Context("events within 24h for same PVC", func() {
		It("should return false when budget is exhausted", func() {
			// 3 events for same PVC within last hour
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 1*time.Hour),
				makeResizeEvent("cluster-1", 2*time.Hour),
				makeResizeEvent("cluster-1", 3*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeFalse(),
				"3 events with maxActions=3 should exhaust budget")
		})

		It("should return true when budget remains", func() {
			// 2 events for same PVC within last hour
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 1*time.Hour),
				makeResizeEvent("cluster-1", 2*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeTrue(),
				"2 events with maxActions=3 should have budget remaining")
		})
	})

	Context("events older than 24h", func() {
		It("should ignore old events and return true", func() {
			// 3 events for same PVC but all older than 24h
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 25*time.Hour),
				makeResizeEvent("cluster-1", 26*time.Hour),
				makeResizeEvent("cluster-1", 27*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeTrue(),
				"events older than 24h should be ignored")
		})
	})

	Context("events for different PVC", func() {
		It("should not count events for other PVCs", func() {
			// 3 events for cluster-2, but checking cluster-1
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-2", 1*time.Hour),
				makeResizeEvent("cluster-2", 2*time.Hour),
				makeResizeEvent("cluster-2", 3*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeTrue(),
				"events for different PVC should not affect budget")
		})
	})

	Context("mixed old and new events", func() {
		It("should only count recent events", func() {
			// 2 events 25h ago + 2 events 1h ago = only 2 within window
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 25*time.Hour),
				makeResizeEvent("cluster-1", 26*time.Hour),
				makeResizeEvent("cluster-1", 1*time.Hour),
				makeResizeEvent("cluster-1", 2*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeTrue(),
				"only 2 events within 24h window, maxActions=3 should have budget")
		})

		It("should return false when recent events exceed budget", func() {
			// 2 events 25h ago + 3 events 1h ago = 3 within window
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 25*time.Hour),
				makeResizeEvent("cluster-1", 26*time.Hour),
				makeResizeEvent("cluster-1", 1*time.Hour),
				makeResizeEvent("cluster-1", 2*time.Hour),
				makeResizeEvent("cluster-1", 3*time.Hour),
			}

			result := HasBudget(cluster, "cluster-1", 3)

			Expect(result).To(BeFalse(),
				"3 events within 24h window, maxActions=3 should exhaust budget")
		})
	})

	Context("maxActions=0", func() {
		It("should always return false", func() {
			// Even with no events, maxActions=0 means no budget
			result := HasBudget(cluster, "cluster-1", 0)

			Expect(result).To(BeFalse(),
				"maxActions=0 should always return false (no budget)")
		})
	})

	Context("maxActions negative", func() {
		It("should always return false", func() {
			result := HasBudget(cluster, "cluster-1", -1)

			Expect(result).To(BeFalse(),
				"negative maxActions should always return false")
		})
	})

	Context("boundary: event exactly at 24h cutoff", func() {
		It("should exclude events at exactly 24h ago", func() {
			// Event exactly 24h ago should be just outside the window
			// (cutoff is time.Now().Add(-24 * time.Hour), events after cutoff count)
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 24*time.Hour),
				makeResizeEvent("cluster-1", 23*time.Hour+59*time.Minute), // just inside
			}

			result := HasBudget(cluster, "cluster-1", 2)

			// The 24h event is at the cutoff boundary. Since we use After(cutoff),
			// events exactly at 24h are excluded, and events just inside are included.
			// Only 1 event should count, so budget should be available.
			Expect(result).To(BeTrue(),
				"event exactly at 24h cutoff should be excluded, leaving budget")
		})
	})

	Context("mixed PVCs and times", func() {
		It("should correctly filter by both PVC name and time", func() {
			cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
				makeResizeEvent("cluster-1", 1*time.Hour),  // counts
				makeResizeEvent("cluster-2", 1*time.Hour),  // wrong PVC
				makeResizeEvent("cluster-1", 25*time.Hour), // too old
				makeResizeEvent("cluster-1", 2*time.Hour),  // counts
				makeResizeEvent("cluster-3", 2*time.Hour),  // wrong PVC
			}

			result := HasBudget(cluster, "cluster-1", 3)

			// Only 2 events for cluster-1 within 24h
			Expect(result).To(BeTrue(),
				"should only count events matching both PVC name and time window")
		})
	})
})

// makeResizeEvent creates a test AutoResizeEvent with the given PVC name
// and age (how long ago the event occurred).
func makeResizeEvent(pvcName string, age time.Duration) apiv1.AutoResizeEvent {
	return apiv1.AutoResizeEvent{
		Timestamp:    metav1.NewTime(time.Now().Add(-age)),
		PVCName:      pvcName,
		InstanceName: pvcName,
		VolumeType:   "data",
		Result:       "success",
	}
}
