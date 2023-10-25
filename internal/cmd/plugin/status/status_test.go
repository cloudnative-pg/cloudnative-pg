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

package status

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getPrimaryStartTime", func() {
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
			Expect(getPrimaryStartTime(cluster)).To(Equal(""))
		})
	})

	Context("when CurrentPrimaryTimestamp is valid", func() {
		BeforeEach(func() {
			now := time.Now()
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: now.Format(metav1.RFC3339Micro),
				},
			}
		})

		It("should return the formatted timestamp", func() {
			uptime := time.Since(time.Now()) // It won't be exactly zero, but close
			expected := fmt.Sprintf("%s (uptime %s)", time.Now().Round(time.Second), uptime.Round(time.Second))
			Expect(getPrimaryStartTime(cluster)).To(Equal(expected))
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
			Expect(getPrimaryStartTime(cluster)).To(ContainSubstring("error"))
		})
	})
})
