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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getPrimaryPromotionTime", func() {
	var cluster *apiv1.Cluster

	Context("when CurrentPrimaryTimestamp is empty", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "",
				},
			}
		})

		It("should return an empty string", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(Equal(""))
		})
	})

	Context("when CurrentPrimaryTimestamp is valid", func() {
		It("should return the formatted timestamp", func() {
			now := time.Now()
			uptime := 1 * time.Hour
			currentPrimaryTimestamp := now.Add(-uptime)

			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: currentPrimaryTimestamp.Format(metav1.RFC3339Micro),
				},
			}

			expected := fmt.Sprintf("%s (%s)", currentPrimaryTimestamp.Round(time.Second), uptime)
			Expect(getPrimaryPromotionTimeIdempotent(cluster, now)).To(Equal(expected))
		})
	})

	Context("when CurrentPrimaryTimestamp is invalid", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "invalid timestamp",
				},
			}
		})

		It("should return the error message", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(ContainSubstring("error"))
		})
	})
})

var _ = Describe("getWalArchivingStatus", func() {
	It("should return 'Disabled' when WAL archiving is disabled", func() {
		result := getWalArchivingStatus(false, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})

	It("should return 'OK' when archiving is working", func() {
		result := getWalArchivingStatus(true, "", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Failing' when there is a failed WAL", func() {
		result := getWalArchivingStatus(false, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("Failing"))
	})

	It("should return 'OK' even when there is a failed WAL if archiving is working", func() {
		result := getWalArchivingStatus(true, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Starting Up' when archiving hasn't started yet", func() {
		result := getWalArchivingStatus(false, "", false)
		Expect(result).To(ContainSubstring("Starting Up"))
	})

	It("should prioritize 'Disabled' over other statuses", func() {
		// Even if archiving is working, disabled should take precedence
		result := getWalArchivingStatus(true, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})
})

var _ = Describe("groupInstancesByFailureDomain", func() {
	const (
		zoneLabel   = "topology.kubernetes.io/zone"
		regionLabel = "topology.kubernetes.io/region"
		primary     = "cluster-1"
		replica1    = "cluster-2"
		replica2    = "cluster-3"
	)

	makeCluster := func(
		failureDomainKey []string,
		instances map[apiv1.PodName]apiv1.PodTopologyLabels,
		extracted bool,
	) *apiv1.Cluster {
		cluster := &apiv1.Cluster{}
		cluster.Status.CurrentPrimary = primary
		cluster.Status.Topology = apiv1.Topology{
			SuccessfullyExtracted: extracted,
			Instances:             instances,
		}
		if failureDomainKey != nil {
			cluster.Spec.PostgresConfiguration.Synchronous = &apiv1.SynchronousReplicaConfiguration{
				FailureDomainKey: failureDomainKey,
			}
		}
		return cluster
	}

	It("returns nil when failureDomainKey is not set", func() {
		cluster := makeCluster(nil, map[apiv1.PodName]apiv1.PodTopologyLabels{
			primary: {zoneLabel: "az1"},
		}, true)
		Expect(groupInstancesByFailureDomain(cluster)).To(BeNil())
	})

	It("returns nil when topology extraction failed", func() {
		cluster := makeCluster([]string{zoneLabel}, nil, false)
		Expect(groupInstancesByFailureDomain(cluster)).To(BeNil())
	})

	It("groups instances into two domains with a single key", func() {
		cluster := makeCluster([]string{zoneLabel}, map[apiv1.PodName]apiv1.PodTopologyLabels{
			primary:  {zoneLabel: "az1"},
			replica1: {zoneLabel: "az2"},
			replica2: {zoneLabel: "az1"},
		}, true)

		groups := groupInstancesByFailureDomain(cluster)
		Expect(groups).To(HaveLen(2))

		// az1 comes first (cluster-1 < cluster-2 in sort order)
		Expect(groups[0].display).To(Equal("az1"))
		Expect(groups[0].instances).To(ConsistOf(primary, replica2))
		Expect(groups[0].hasPrimary).To(BeTrue())

		Expect(groups[1].display).To(Equal("az2"))
		Expect(groups[1].instances).To(ConsistOf(replica1))
		Expect(groups[1].hasPrimary).To(BeFalse())
	})

	It("produces a single group when all instances share the same domain", func() {
		cluster := makeCluster([]string{zoneLabel}, map[apiv1.PodName]apiv1.PodTopologyLabels{
			primary:  {zoneLabel: "az1"},
			replica1: {zoneLabel: "az1"},
		}, true)

		groups := groupInstancesByFailureDomain(cluster)
		Expect(groups).To(HaveLen(1))
		Expect(groups[0].hasPrimary).To(BeTrue())
	})

	It("formats the display value as key=value pairs for multiple keys", func() {
		cluster := makeCluster([]string{zoneLabel, regionLabel}, map[apiv1.PodName]apiv1.PodTopologyLabels{
			primary:  {zoneLabel: "az1", regionLabel: "us-east-1"},
			replica1: {zoneLabel: "az2", regionLabel: "us-east-1"},
		}, true)

		groups := groupInstancesByFailureDomain(cluster)
		Expect(groups).To(HaveLen(2))
		Expect(groups[0].display).To(ContainSubstring("topology.kubernetes.io/zone=az1"))
		Expect(groups[0].display).To(ContainSubstring("topology.kubernetes.io/region=us-east-1"))
	})
})
