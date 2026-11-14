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

package controller

import (
	"os"
	"path/filepath"
	"time"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("write path check reconciliation", func() {
	const podName = "cluster-example-1"

	var (
		r       *InstanceReconciler
		acquire *fakeLeaseAcquirer
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		acquire = &fakeLeaseAcquirer{}
		pgInstance := postgres.NewInstance().
			WithNamespace("default").
			WithPodName(podName).
			WithClusterName("cluster-example")
		pgInstance.PgData = GinkgoT().TempDir()
		r = &InstanceReconciler{instance: pgInstance, primaryLeaseAcquirer: acquire}

		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{Probes: &apiv1.ProbesConfiguration{
				Liveness: &apiv1.LivenessProbe{WritePathCheck: &apiv1.WritePathCheckConfiguration{
					Enabled: ptr.To(true),
				}},
			}},
			Status: apiv1.ClusterStatus{TargetPrimary: podName, CurrentPrimary: podName},
		}
	})

	stall := func() {
		now := time.Now()
		r.instance.WritePath.BeatStarted(now.Add(-time.Hour))
		Expect(r.instance.WritePath.Check(now, time.Minute)).To(BeTrue())
	}

	Describe("reconcileStalledWritePath", func() {
		It("does nothing while the write path is healthy", func(ctx SpecContext) {
			Expect(r.reconcileStalledWritePath(ctx, cluster)).To(BeNil())
			Expect(r.instance.WritePath.Latched()).To(BeFalse())
		})

		It("skips the rest of the reconciliation while stalled and still the target primary", func(ctx SpecContext) {
			stall()
			result := r.reconcileStalledWritePath(ctx, cluster)
			Expect(result).ToNot(BeNil())
			Expect(result.RequeueAfter).To(Equal(time.Second))
			Expect(r.instance.WritePath.Latched()).To(BeFalse())
			// The lease is neither yielded nor taken here: the write path monitor stops
			// the renewal, and the regular reconciliation takes it back after the stall
			Expect(acquire.yields).To(BeZero())
			Expect(acquire.stopRenewals).To(BeZero())
		})

		It("latches the stall once the target primary is another instance", func(ctx SpecContext) {
			stall()
			cluster.Status.TargetPrimary = apiv1.PendingFailoverMarker
			result := r.reconcileStalledWritePath(ctx, cluster)
			Expect(result).ToNot(BeNil())
			Expect(result.RequeueAfter).To(Equal(time.Second))
			Expect(r.instance.WritePath.Latched()).To(BeTrue())
		})

		It("leaves a stalled replica to the regular reconciliation", func(ctx SpecContext) {
			stall()
			cluster.Status.TargetPrimary = "cluster-example-2"
			Expect(os.WriteFile(filepath.Join(r.instance.PgData, "standby.signal"), nil, 0o600)).To(Succeed())
			Expect(r.reconcileStalledWritePath(ctx, cluster)).To(BeNil())
			Expect(r.instance.WritePath.Latched()).To(BeTrue())
		})
	})

	Describe("reconcileWalReceiverDuringFailover", func() {
		BeforeEach(func() {
			cluster.Status.TargetPrimary = apiv1.PendingFailoverMarker
			cluster.Status.CurrentPrimary = "cluster-example-2"
			// a replica
			Expect(os.WriteFile(filepath.Join(r.instance.PgData, "standby.signal"), nil, 0o600)).To(Succeed())
		})

		It("does nothing when the write path check is disabled", func(ctx SpecContext) {
			cluster.Spec.Probes.Liveness.WritePathCheck.Enabled = ptr.To(false)
			Expect(r.reconcileWalReceiverDuringFailover(ctx, cluster)).To(BeNil())
		})

		It("does nothing when no failover is pending", func(ctx SpecContext) {
			cluster.Status.TargetPrimary = "cluster-example-3"
			Expect(r.reconcileWalReceiverDuringFailover(ctx, cluster)).To(BeNil())
		})

		It("does nothing on the current primary", func(ctx SpecContext) {
			cluster.Status.CurrentPrimary = podName
			Expect(r.reconcileWalReceiverDuringFailover(ctx, cluster)).To(BeNil())
		})

		It("does nothing on a primary", func(ctx SpecContext) {
			Expect(os.Remove(filepath.Join(r.instance.PgData, "standby.signal"))).To(Succeed())
			Expect(r.reconcileWalReceiverDuringFailover(ctx, cluster)).To(BeNil())
		})

		It("does nothing in a replica cluster", func(ctx SpecContext) {
			cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true)}
			Expect(r.reconcileWalReceiverDuringFailover(ctx, cluster)).To(BeNil())
		})

		It("keeps polling on a replica while the failover is pending, even if the database can't be reached",
			func(ctx SpecContext) {
				result := r.reconcileWalReceiverDuringFailover(ctx, cluster)
				Expect(result).ToNot(BeNil())
				Expect(result.RequeueAfter).To(Equal(time.Second))
			})
	})
})
