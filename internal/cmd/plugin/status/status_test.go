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

	Context("when WAL archiving is not configured", func() {
		It("should return Disabled", func() {
			result := getWalArchivingStatus(
				false,
				"",
				false,
			)

			Expect(result).To(ContainSubstring("Disabled"))
		})
	})

	Context("when WAL archiving is working", func() {
		It("should return OK", func() {
			result := getWalArchivingStatus(
				true,
				"",
				true,
			)

			Expect(result).To(ContainSubstring("OK"))
		})
	})

	Context("when WAL archiving is failing", func() {
		It("should return Failing", func() {
			result := getWalArchivingStatus(
				false,
				"0000000101",
				true,
			)

			Expect(result).To(ContainSubstring("Failing"))
		})
	})

	Context("when WAL archiving is configured but not yet started", func() {
		It("should return Starting Up", func() {
			result := getWalArchivingStatus(
				false,
				"",
				true,
			)

			Expect(result).To(ContainSubstring("Starting Up"))
		})
	})
})
