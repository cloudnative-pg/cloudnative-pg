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

package postgres

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Partition", func() {
	errStatusEndpointFailing := fmt.Errorf("status endpoint failing")

	It("puts an all-reporting list entirely into Reporting", func() {
		list := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "one"}}},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "two"}}},
			},
		}

		set := list.Partition()
		Expect(set.Reporting.Items).To(HaveLen(2))
		Expect(set.ReadyButSilent).To(BeEmpty())
		Expect(set.NotReady).To(BeEmpty())
	})

	It("puts an all-silent list entirely into ReadyButSilent or NotReady", func() {
		list := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "ready-silent"}},
					IsPodReady: true,
					Error:      errStatusEndpointFailing,
				},
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "not-ready-silent"}},
					IsPodReady: false,
					Error:      errStatusEndpointFailing,
				},
			},
		}

		set := list.Partition()
		Expect(set.Reporting.Items).To(BeEmpty())
		Expect(set.ReadyButSilent).To(HaveLen(1))
		Expect(set.NotReady).To(HaveLen(1))
	})

	It("handles an empty list", func() {
		set := PostgresqlStatusList{}.Partition()
		Expect(set.Reporting.Items).To(BeEmpty())
		Expect(set.ReadyButSilent).To(BeEmpty())
		Expect(set.NotReady).To(BeEmpty())
	})

	It("splits a mixed list correctly", func() {
		list := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:  &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "reporting"}},
					Node: "node-1",
				},
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "ready-silent"}},
					Node:       "node-2",
					IsPodReady: true,
					Error:      errStatusEndpointFailing,
				},
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "not-ready-silent"}},
					Node:       "node-3",
					IsPodReady: false,
					Error:      errStatusEndpointFailing,
				},
			},
		}

		set := list.Partition()
		Expect(set.Reporting.Items).To(HaveLen(1))
		Expect(set.Reporting.Items[0].Pod.Name).To(Equal("reporting"))

		Expect(set.ReadyButSilent).To(HaveLen(1))
		Expect(set.ReadyButSilent[0].Pod.Name).To(Equal("ready-silent"))
		Expect(set.ReadyButSilent[0].Node).To(Equal("node-2"))
		Expect(set.ReadyButSilent[0].Error).To(MatchError(errStatusEndpointFailing))

		Expect(set.NotReady).To(HaveLen(1))
		Expect(set.NotReady[0].Pod.Name).To(Equal("not-ready-silent"))
		Expect(set.NotReady[0].Node).To(Equal("node-3"))
		Expect(set.NotReady[0].Error).To(MatchError(errStatusEndpointFailing))
	})

	It("never lets a reporting item leak into either silent group", func() {
		list := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "reporting-but-not-ready"}},
					IsPodReady: false,
					Error:      nil,
				},
			},
		}

		set := list.Partition()
		Expect(set.Reporting.Items).To(HaveLen(1))
		Expect(set.ReadyButSilent).To(BeEmpty())
		Expect(set.NotReady).To(BeEmpty())
	})
})
