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

package postgres

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL status", func() {
	errCannotConnectToPostgres := fmt.Errorf("cannot connect to PostgreSQL")

	list := PostgresqlStatusList{
		Items: []PostgresqlStatus{
			{
				Pod:   corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-04"}},
				Error: errCannotConnectToPostgres,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-06"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				Error:       errCannotConnectToPostgres,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-30"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
			},
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-40"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/23",
			},
		},
	}

	It("checks for errors in the Pod status", func() {
		greenList := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary:   false,
					ReceivedLsn: "1/21",
				},
				{
					Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
					IsPrimary: true,
				},
			},
		}

		Expect(list.IsComplete()).To(BeFalse())
		Expect(greenList.IsComplete()).To(BeTrue())
	})

	It("checks for pods on which we are upgrading the instance manager", func() {
		podList := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary:   false,
					ReceivedLsn: "1/21",
				},
				{
					Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
					IsPrimary: true,
				},
			},
		}
		Expect(podList.ArePodsUpgradingInstanceManager()).To(BeFalse())
		podList.Items[0].IsInstanceManagerUpgrading = true
		Expect(podList.ArePodsUpgradingInstanceManager()).To(BeTrue())
	})

	It("checks for pods on which fencing is enabled", func() {
		podList := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary: false,
				},
				{
					Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
					IsPrimary: true,
				},
			},
		}
		Expect(podList.ReportingMightBeUnavailable(podList.Items[0].Pod.Name)).To(BeFalse())
		Expect(podList.ReportingMightBeUnavailable(podList.Items[1].Pod.Name)).To(BeFalse())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(0))
		podList.Items[1].MightBeUnavailable = true
		Expect(podList.ReportingMightBeUnavailable(podList.Items[0].Pod.Name)).To(BeFalse())
		Expect(podList.ReportingMightBeUnavailable(podList.Items[1].Pod.Name)).To(BeTrue())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(1))
		podList.Items[0].MightBeUnavailable = true
		Expect(podList.ReportingMightBeUnavailable(podList.Items[0].Pod.Name)).To(BeTrue())
		Expect(podList.ReportingMightBeUnavailable(podList.Items[1].Pod.Name)).To(BeTrue())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(2))
	})

	Describe("when sorted", func() {
		sort.Sort(&list)

		It("primary servers are come first", func() {
			Expect(list.Items[0].IsPrimary).To(BeTrue())
			Expect(list.Items[0].Pod.Name).To(Equal("server-10"))
		})

		It("contains the more updated server as the second element", func() {
			Expect(list.Items[1].IsPrimary).To(BeFalse())
			Expect(list.Items[1].Pod.Name).To(Equal("server-40"))
		})

		It("contains other servers considering their status", func() {
			Expect(list.Items[2].Pod.Name).To(Equal("server-30"))
			Expect(list.Items[3].Pod.Name).To(Equal("server-20"))
		})

		It("put the non-ready servers after the ready ones", func() {
			Expect(list.Items[4].Pod.Name).To(Equal("server-06"))
			Expect(list.Items[4].Pod.Name).To(Equal("server-06"))
		})

		It("put the incomplete statuses at the bottom of the list", func() {
			Expect(list.Items[5].Pod.Name).To(Equal("server-04"))
			Expect(list.Items[5].Pod.Name).To(Equal("server-04"))
		})
	})
})

var _ = Describe("PostgreSQL status real", func() {
	f, err := os.Open("testdata/lsn_overflow.json")
	defer func() {
		_ = f.Close()
	}()
	Expect(err).ToNot(HaveOccurred())

	var list PostgresqlStatusList
	err = json.NewDecoder(f).Decode(&list)
	Expect(err).ToNot(HaveOccurred())

	Describe("when sorted", func() {
		sort.Sort(&list)

		It("most advanced server comes first", func() {
			Expect(list.Items[0].IsPrimary).To(BeFalse())
			Expect(list.Items[0].Pod.Name).To(Equal("sandbox-3"))
		})

		// order again to verify that the result is stable
		sort.Sort(&list)

		It("most advanced server comes first (stable order)", func() {
			Expect(list.Items[0].IsPrimary).To(BeFalse())
			Expect(list.Items[0].Pod.Name).To(Equal("sandbox-3"))
		})
	})
})
