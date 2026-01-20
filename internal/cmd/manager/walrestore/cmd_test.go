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

package walrestore

import (
	barmanRestorer "github.com/cloudnative-pg/barman-cloud/pkg/restorer"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Function isStreamingAvailable", func() {
	It("returns false if cluster is nil", func() {
		Expect(isStreamingAvailable(nil, "testPod")).To(BeFalse())
	})

	It("returns true if current primary does not match the given pod name", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
		}
		Expect(isStreamingAvailable(&cluster, "replicaPod")).To(BeTrue())
	})

	It("returns false if current primary matches the given pod name and this is not a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns false if there are not connection parameters and this is a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "clusterSource",
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns false if this is a replica cluster, "+
		"but replica cluster source does not match external cluster name", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "wrongNameClusterSource",
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeFalse())
	})

	It("returns true if the external cluster has streaming connection and this is a replica cluster", func() {
		cluster := apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primaryPod",
			},
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name:                 "clusterSource",
						ConnectionParameters: map[string]string{"dbname": "test"},
					},
				},
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeTrue())
	})
})

var _ = Describe("validateTimelineHistoryFile", func() {
	It("should allow regular WAL files to pass through", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "000000010000000000000001", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow invalid history filenames to pass through", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "invalid.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow primary to download any timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "primary-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow target primary (being promoted) to download any timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "old-primary",
				TargetPrimary:  "new-primary",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "new-primary")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow replica to download current timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000021.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should allow replica to download past timeline", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000010.history", cluster, "replica-pod")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should reject future timeline for replica", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     33,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000022.history", cluster, "replica-pod")
		Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
	})

	It("should reject future timeline for replica with large timeline difference", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     5,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000064.history", cluster, "replica-pod")
		Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
	})

	It("should reject any timeline when cluster timeline is not yet set", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "primary-pod",
				TimelineID:     0,
			},
		}

		err := validateTimelineHistoryFile(ctx, "00000001.history", cluster, "replica-pod")
		Expect(err).To(Equal(barmanRestorer.ErrWALNotFound))
	})
})
