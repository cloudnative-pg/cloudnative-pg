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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PostgreSQL status", func() {
	It("checks for errors in the Pod status", func() {
		list := PostgresqlStatusList{
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
				IsReady:     true,
			},
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
				IsReady:   true,
			},
		}

		Expect(list.IsComplete()).To(BeTrue())

		list = append(list, PostgresqlStatus{
			Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-30"}},
			IsPrimary:   false,
			ReceivedLsn: "1/21",
			IsReady:     false,
			Error:       fmt.Errorf("cannot find postgres container"),
		},
			PostgresqlStatus{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-40"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
				IsReady:     false,
			})

		Expect(list.IsComplete()).To(BeFalse())
	})

	It("checks for pods on which we are upgrading the instance manager", func() {
		podList := PostgresqlStatusList{
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
				IsReady:     true,
			},
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
				IsReady:   true,
			},
		}
		Expect(podList.ArePodsUpgradingInstanceManager()).To(BeFalse())
		podList[0].IsInstanceManagerUpgrading = true
		Expect(podList.ArePodsUpgradingInstanceManager()).To(BeTrue())
	})

	It("checks for pods on which fencing is enabled", func() {
		podList := PostgresqlStatusList{
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary: false,
				IsReady:   true,
			},
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
				IsReady:   true,
			},
		}
		Expect(podList.ReportingMightBeUnavailable(podList[0].Pod.Name)).To(BeFalse())
		Expect(podList.ReportingMightBeUnavailable(podList[1].Pod.Name)).To(BeFalse())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(0))
		podList[1].MightBeUnavailable = true
		Expect(podList.ReportingMightBeUnavailable(podList[0].Pod.Name)).To(BeFalse())
		Expect(podList.ReportingMightBeUnavailable(podList[1].Pod.Name)).To(BeTrue())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(1))
		podList[0].MightBeUnavailable = true
		Expect(podList.ReportingMightBeUnavailable(podList[0].Pod.Name)).To(BeTrue())
		Expect(podList.ReportingMightBeUnavailable(podList[1].Pod.Name)).To(BeTrue())
		Expect(podList.InstancesReportingStatus()).To(BeEquivalentTo(2))
	})

	Context("when sorted", func() {
		list := PostgresqlStatusList{
			{
				Pod:     corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-04"}},
				Error:   fmt.Errorf("cannot find postgres container"),
				IsReady: true,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-06"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				IsReady:     false,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-30"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				IsReady:     true,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
				IsReady:     true,
			},
			{
				Pod:       corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
				IsReady:   true,
			},
			{
				Pod:         corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-40"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/23",
				IsReady:     true,
			},
		}

		sort.Sort(&list)

		It("primary servers are coming first", func() {
			Expect(list[0].IsPrimary).To(BeTrue())
			Expect(list[0].Pod.Name).To(Equal("server-10"))
		})

		It("contains the more updated server as the second element", func() {
			Expect(list[1].IsPrimary).To(BeFalse())
			Expect(list[1].Pod.Name).To(Equal("server-40"))
		})

		It("contains other servers considering their status", func() {
			Expect(list[2].Pod.Name).To(Equal("server-30"))
			Expect(list[3].Pod.Name).To(Equal("server-20"))
		})

		It("put the non-ready servers after the ready ones", func() {
			Expect(list[4].Pod.Name).To(Equal("server-06"))
			Expect(list[4].Pod.Name).To(Equal("server-06"))
		})

		It("put the incomplete statuses at the bottom of the list", func() {
			Expect(list[5].Pod.Name).To(Equal("server-04"))
			Expect(list[5].Pod.Name).To(Equal("server-04"))
		})
	})
})

var _ = Describe("PostgreSQL status real", func() {
	var list PostgresqlStatusList
	It("can parse the JSON status list", func() {
		f, err := os.Open("testdata/lsn_overflow.json")
		Expect(err).ToNot(HaveOccurred())
		err = json.NewDecoder(f).Decode(&list)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).To(Succeed())
		Expect(list).To(HaveLen(2))
	})

	Context("when sorted", func() {
		It("most advanced server comes first", func() {
			sort.Sort(list)
			Expect(list).To(HaveLen(2))
			Expect(list[0].IsPrimary).To(BeFalse())
			Expect(list[0].Pod.Name).To(Equal("sandbox-3"))
		})

		It("most advanced server comes first (stable order)", func() {
			// order again to verify that the result is stable
			sort.Sort(list)
			Expect(list[0].IsPrimary).To(BeFalse())
			Expect(list[0].Pod.Name).To(Equal("sandbox-3"))
		})
	})
})
