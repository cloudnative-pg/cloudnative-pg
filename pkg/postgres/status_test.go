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
	"encoding/json"
	"fmt"
	"os"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL status", func() {
	errCannotConnectToPostgres := fmt.Errorf("cannot connect to PostgreSQL")

	list := PostgresqlStatusList{
		Items: []PostgresqlStatus{
			{
				Pod:   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-04"}},
				Error: errCannotConnectToPostgres,
			},
			{
				Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-06"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
				Error:       errCannotConnectToPostgres,
			},
			{
				Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-30"}},
				IsPrimary:   false,
				ReceivedLsn: "1/23",
				ReplayLsn:   "1/22",
			},
			{
				Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
				IsPrimary:   false,
				ReceivedLsn: "1/21",
			},
			{
				Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
				IsPrimary: true,
			},
			{
				Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-40"}},
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
					Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary:   false,
					ReceivedLsn: "1/21",
				},
				{
					Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
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
					Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary:   false,
					ReceivedLsn: "1/21",
				},
				{
					Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
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
					Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-20"}},
					IsPrimary: false,
				},
				{
					Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server-10"}},
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

	It("Correctly handles LSN parser errors", func() {
		podList := PostgresqlStatusList{
			Items: []PostgresqlStatus{
				{
					Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p-1"}},
					CurrentLsn:  "42A/E0F07C8",
					CurrentWAL:  "0000001B0000042A0000000E",
					ReceivedLsn: "",
					ReplayLsn:   "",
					IsPrimary:   true,
					Error:       nil,
				},
				{
					Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p-2"}},
					CurrentLsn:  "",
					CurrentWAL:  "",
					ReceivedLsn: "42A/E0F0AA0",
					ReplayLsn:   "42A/E0F0AA0",
					IsPrimary:   false,
					Error:       nil,
				},
				{
					Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p-3"}},
					CurrentLsn:  "",
					CurrentWAL:  "",
					ReceivedLsn: "",
					ReplayLsn:   "42A/DFFFFF8",
					IsPrimary:   false,
					Error:       nil,
				},
			},
		}
		sort.Sort(&podList)

		// p-1 is the first entry of the list because it is the primary node
		// p-2 is the second entry of the list because it is more advanced
		// from the replication point-of-view ("42A/E0F0AA0" > "42A/DFFFFF8")

		Expect(podList.Items[0].Pod.Name).To(Equal("p-1"))
		Expect(podList.Items[1].Pod.Name).To(Equal("p-2"))
		Expect(podList.Items[2].Pod.Name).To(Equal("p-3"))
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

var _ = Describe("IsPodReadyAndNotReporting", func() {
	errStatusEndpointFailing := fmt.Errorf("status endpoint failing")

	podList := PostgresqlStatusList{
		Items: []PostgresqlStatus{
			{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "primary"}},
				IsPodReady: true,
				Error:      errStatusEndpointFailing,
			},
			{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-reporting"}},
				IsPodReady: true,
				Error:      nil,
			},
			{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replica-not-ready"}},
				IsPodReady: false,
				Error:      errStatusEndpointFailing,
			},
		},
	}

	It("returns true and the underlying error when the pod is ready but the status endpoint is failing", func() {
		ok, err := podList.IsPodReadyAndNotReporting("primary")
		Expect(ok).To(BeTrue())
		Expect(err).To(MatchError(errStatusEndpointFailing))
	})

	It("returns false and a nil error when the pod is ready and reporting", func() {
		ok, err := podList.IsPodReadyAndNotReporting("replica-reporting")
		Expect(ok).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns false and a nil error when the pod is not ready and not reporting", func() {
		ok, err := podList.IsPodReadyAndNotReporting("replica-not-ready")
		Expect(ok).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns false and a nil error when the pod is not in the list", func() {
		ok, err := podList.IsPodReadyAndNotReporting("unknown")
		Expect(ok).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("AreWalReceiversDown", func() {
	errStatusEndpointFailing := fmt.Errorf("status endpoint failing")

	newList := func(items ...PostgresqlStatus) PostgresqlStatusList {
		return PostgresqlStatusList{Items: items}
	}

	primary := PostgresqlStatus{
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "primary"}},
		IsPrimary: true,
	}

	It("returns true when every standby has reported its WAL receiver as down", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-1"}},
				IsWalReceiverActive: false,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeTrue())
		Expect(unknown).To(BeEmpty())
	})

	It("returns false when a standby is confirmed to still have an active WAL receiver", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-1"}},
				IsWalReceiverActive: true,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeFalse())
		Expect(unknown).To(BeEmpty())
	})

	It("returns false and reports the standby when its status is unknown (errored but Ready)", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-1"}},
				IsPodReady: true,
				Error:      errStatusEndpointFailing,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeFalse())
		Expect(unknown).To(ConsistOf("standby-1"))
	})

	It("treats an errored and not-Ready standby as confirmed down, not unknown", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-1"}},
				IsPodReady: false,
				Error:      errStatusEndpointFailing,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeTrue())
		Expect(unknown).To(BeEmpty())
	})

	It("blocks on the mix of one confirmed-down standby and one Ready-but-erroring standby", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-confirmed-down"}},
				IsWalReceiverActive: false,
			},
			PostgresqlStatus{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-unknown"}},
				IsPodReady: true,
				Error:      errStatusEndpointFailing,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeFalse())
		Expect(unknown).To(ConsistOf("standby-unknown"))
	})

	It("does not stall a genuine node-failure failover, where the dead standby is errored and not Ready", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-alive"}},
				IsWalReceiverActive: false,
			},
			PostgresqlStatus{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-dead-node"}},
				IsPodReady: false,
				Error:      errStatusEndpointFailing,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeTrue())
		Expect(unknown).To(BeEmpty())
	})

	It("does not finalize the failover even when the unknown standby holds the highest last-seen LSN", func() {
		// Adversarial: the errored standby is the one that would otherwise be
		// elected first by Less() (highest ReceivedLsn/ReplayLsn), but since
		// its status is unknown the gate must still block on it rather than
		// letting the sort order imply it is safe to proceed.
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-behind"}},
				IsWalReceiverActive: false,
				ReceivedLsn:         "1/10",
				ReplayLsn:           "1/10",
			},
			PostgresqlStatus{
				Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-ahead-unknown"}},
				IsPodReady:  true,
				Error:       errStatusEndpointFailing,
				ReceivedLsn: "1/99",
				ReplayLsn:   "1/99",
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeFalse())
		Expect(unknown).To(ConsistOf("standby-ahead-unknown"))
	})

	// newSilentStandby builds a PostgresqlStatus for an instance whose
	// /pg/status probe failed, going through AddPod so IsPodReady is derived
	// from a real Ready condition on the Pod rather than hardcoded, which is
	// what the production code path (partitioning by Reporting/ReadyButSilent)
	// actually relies on.
	newSilentStandby := func(name string, probeErr error) PostgresqlStatus {
		status := PostgresqlStatus{Error: probeErr}
		status.AddPod(corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		})
		return status
	}

	It("does not block on the primary's own silence", func() {
		silentPrimary := newSilentStandby(primary.Pod.Name, errStatusEndpointFailing)
		silentPrimary.IsPrimary = true

		list := newList(
			silentPrimary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-1"}},
				IsWalReceiverActive: false,
			},
		)

		down, unknown := list.Partition().AreWalReceiversDown(silentPrimary.Pod.Name)
		Expect(down).To(BeTrue())
		Expect(unknown).ToNot(ContainElement(silentPrimary.Pod.Name))
	})

	It("blocks on the active receiver before it would even consider a silent standby", func() {
		list := newList(
			primary,
			PostgresqlStatus{
				Pod:                 &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standby-active"}},
				IsWalReceiverActive: true,
			},
			newSilentStandby("standby-silent", errStatusEndpointFailing),
		)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeFalse())
		Expect(unknown).To(BeEmpty())
	})

	It("does not block a primary-only, single-instance cluster", func() {
		list := newList(primary)

		down, unknown := list.Partition().AreWalReceiversDown(primary.Pod.Name)
		Expect(down).To(BeTrue())
		Expect(unknown).To(BeEmpty())
	})
})

var _ = Describe("Configuration report", func() {
	DescribeTable(
		"Configuration report",
		func(report ConfigurationReport, result *bool) {
			if result == nil {
				Expect(report.IsUniform()).To(BeNil())
			}
			Expect(report.IsUniform()).To(Equal(result))
		},
		Entry(
			"with older and newer instance managers at the same time",
			ConfigurationReport{
				{
					PodName:    "cluster-example-1",
					ConfigHash: "",
				},
				{
					PodName:    "cluster-example-2",
					ConfigHash: "abc",
				},
			},
			nil,
		),
		Entry(
			"with old instance managers",
			ConfigurationReport{
				{
					PodName:    "cluster-example-1",
					ConfigHash: "",
				},
				{
					PodName:    "cluster-example-2",
					ConfigHash: "",
				},
			},
			nil,
		),
		Entry(
			"with instance managers that are reporting different configurations",
			ConfigurationReport{
				{
					PodName:    "cluster-example-1",
					ConfigHash: "abc",
				},
				{
					PodName:    "cluster-example-2",
					ConfigHash: "def",
				},
			},
			ptr.To(false),
		),
		Entry(
			"with instance manager that are reporting the same configuration",
			ConfigurationReport{
				{
					PodName:    "cluster-example-1",
					ConfigHash: "abc",
				},
				{
					PodName:    "cluster-example-2",
					ConfigHash: "abc",
				},
			},
			ptr.To(true),
		),
	)
})
