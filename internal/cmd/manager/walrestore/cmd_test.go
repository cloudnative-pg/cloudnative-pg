/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package walrestore

import (
	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
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
					Enabled: true,
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
					Enabled: true,
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
					Enabled: true,
					Source:  "clusterSource",
				},
			},
		}
		Expect(isStreamingAvailable(&cluster, "primaryPod")).To(BeTrue())
	})
})
