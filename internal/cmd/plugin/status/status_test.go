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

	"github.com/logrusorgru/aurora/v4"
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
		Expect(result).To(Equal(aurora.Yellow("Disabled").String()))
	})

	It("should return 'OK' when archiving is working", func() {
		result := getWalArchivingStatus(true, "", false)
		Expect(result).To(Equal(aurora.Green("OK").String()))
	})

	It("should return 'Failing' when there is a failed WAL", func() {
		result := getWalArchivingStatus(false, "000000010000000000000001", false)
		Expect(result).To(Equal(aurora.Red("Failing").String()))
	})

	It("should return 'Starting Up' when archiving hasn't started yet", func() {
		result := getWalArchivingStatus(false, "", false)
		Expect(result).To(Equal(aurora.Yellow("Starting Up").String()))
	})

	It("should prioritize 'Disabled' over other states", func() {
		result := getWalArchivingStatus(true, "000000010000000000000001", true)
		Expect(result).To(Equal(aurora.Yellow("Disabled").String()))
	})
})
