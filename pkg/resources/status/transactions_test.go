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

package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Status transactions", func() {
	Describe("SetClusterReadyCondition", func() {
		It("sets ready condition to true when phase is healthy", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					Phase: apiv1.PhaseHealthy,
				},
			}

			SetClusterReadyCondition(cluster)

			Expect(cluster.Status.Conditions).To(HaveLen(1))
			condition := cluster.Status.Conditions[0]
			Expect(condition.Type).To(Equal(string(apiv1.ConditionClusterReady)))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(string(apiv1.ClusterReady)))
			Expect(condition.Message).To(Equal("Cluster is Ready"))
		})

		It("sets ready condition to false when phase is not healthy", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					Phase: apiv1.PhaseMajorUpgrade,
				},
			}

			SetClusterReadyCondition(cluster)

			Expect(cluster.Status.Conditions).To(HaveLen(1))
			condition := cluster.Status.Conditions[0]
			Expect(condition.Type).To(Equal(string(apiv1.ConditionClusterReady)))
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(string(apiv1.ClusterIsNotReady)))
			Expect(condition.Message).To(Equal("Cluster Is Not Ready"))
		})

		It("initializes conditions slice if nil", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					Phase:      apiv1.PhaseHealthy,
					Conditions: nil,
				},
			}

			SetClusterReadyCondition(cluster)

			Expect(cluster.Status.Conditions).ToNot(BeNil())
			Expect(cluster.Status.Conditions).To(HaveLen(1))
		})
	})

	Describe("SetPhase", func() {
		It("sets the cluster phase and reason", func() {
			cluster := &apiv1.Cluster{}

			SetPhase(apiv1.PhaseMajorUpgrade, "Upgrading to version 17")(cluster)

			Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseMajorUpgrade))
			Expect(cluster.Status.PhaseReason).To(Equal("Upgrading to version 17"))
		})

		It("overwrites existing phase and reason", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					Phase:       apiv1.PhaseHealthy,
					PhaseReason: "All good",
				},
			}

			SetPhase(apiv1.PhaseUpgrade, "Rolling update")(cluster)

			Expect(cluster.Status.Phase).To(Equal(apiv1.PhaseUpgrade))
			Expect(cluster.Status.PhaseReason).To(Equal("Rolling update"))
		})
	})

	Describe("SetImage", func() {
		It("sets the cluster image", func() {
			cluster := &apiv1.Cluster{}

			SetImage("ghcr.io/cloudnative-pg/postgresql:17.2")(cluster)

			Expect(cluster.Status.Image).To(Equal("ghcr.io/cloudnative-pg/postgresql:17.2"))
		})

		It("overwrites existing image", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					Image: "ghcr.io/cloudnative-pg/postgresql:16.5",
				},
			}

			SetImage("ghcr.io/cloudnative-pg/postgresql:17.2")(cluster)

			Expect(cluster.Status.Image).To(Equal("ghcr.io/cloudnative-pg/postgresql:17.2"))
		})
	})

	Describe("SetPGDataImageInfo", func() {
		It("sets the PGData image info", func() {
			cluster := &apiv1.Cluster{}
			imageInfo := &apiv1.ImageInfo{
				Image:        "ghcr.io/cloudnative-pg/postgresql:17.2",
				MajorVersion: 17,
			}

			SetPGDataImageInfo(imageInfo)(cluster)

			Expect(cluster.Status.PGDataImageInfo).ToNot(BeNil())
			Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("ghcr.io/cloudnative-pg/postgresql:17.2"))
			Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(17))
		})

		It("overwrites existing PGData image info", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					PGDataImageInfo: &apiv1.ImageInfo{
						Image:        "ghcr.io/cloudnative-pg/postgresql:16.5",
						MajorVersion: 16,
					},
				},
			}
			newImageInfo := &apiv1.ImageInfo{
				Image:        "ghcr.io/cloudnative-pg/postgresql:17.2",
				MajorVersion: 17,
			}

			SetPGDataImageInfo(newImageInfo)(cluster)

			Expect(cluster.Status.PGDataImageInfo.Image).To(Equal("ghcr.io/cloudnative-pg/postgresql:17.2"))
			Expect(cluster.Status.PGDataImageInfo.MajorVersion).To(Equal(17))
		})
	})

	Describe("SetTimelineID", func() {
		It("sets the cluster timeline ID", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					TimelineID: 5,
				},
			}

			SetTimelineID(10)(cluster)
			Expect(cluster.Status.TimelineID).To(Equal(10))
		})

		It("resets timeline ID to 1 after major upgrade", func() {
			cluster := &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					TimelineID: 2,
				},
			}

			SetTimelineID(1)(cluster)
			Expect(cluster.Status.TimelineID).To(Equal(1))
		})
	})
})
