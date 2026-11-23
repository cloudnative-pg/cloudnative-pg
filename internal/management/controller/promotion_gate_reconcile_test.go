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
	"context"
	"errors"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("promoteAfterGate", func() {
	const (
		namespace   = "default"
		clusterName = "cluster-example"
		podName     = clusterName + "-3"
		oldPrimary  = clusterName + "-2"
	)

	var (
		r        *InstanceReconciler
		acquirer *fakeLeaseAcquirer
		cluster  *apiv1.Cluster
		recorder *record.FakeRecorder

		// promoted records the position in the calls on the lease at which
		// the promotion was run
		promoted [][]string
	)

	// published is what the previous primary left on the lease
	published := func(state utils.PgDataState, lsn string, timeline int) string {
		value, err := utils.LeasePosition{LSN: lsn, Timeline: timeline, State: state, Pod: oldPrimary}.Marshal()
		Expect(err).ToNot(HaveOccurred())
		return value
	}

	BeforeEach(func() {
		acquirer = &fakeLeaseAcquirer{}
		recorder = record.NewFakeRecorder(10)
		promoted = nil

		pgInstance := postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(podName).
			WithClusterName(clusterName)
		pgInstance.PgData = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(pgInstance.PgData, "standby.signal"), nil, 0o600)).To(Succeed())

		r = &InstanceReconciler{
			instance:             pgInstance,
			primaryLeaseAcquirer: acquirer,
			recorder:             recorder,
			readReplicaPosition: func(context.Context) (replicaPosition, error) {
				return replicaPosition{LSN: "0/80000A0", Timeline: 2}, nil
			},
			promoteInstance: func(context.Context) error {
				promoted = append(promoted, append([]string(nil), acquirer.calls...))
				return nil
			},
			waitWalReceiverDown: func(context.Context) error { return nil },
		}

		// A switchover or a failover to this pod is in progress
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Status:     apiv1.ClusterStatus{TargetPrimary: podName, CurrentPrimary: oldPrimary},
		}
	})

	When("the replica has all the data of the previous primary", func() {
		BeforeEach(func() {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
		})

		It("marks the promotion, and then promotes", func(ctx SpecContext) {
			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())

			Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
		})
	})

	When("the replica is missing data of the previous primary", func() {
		BeforeEach(func() {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/9000028", 2)
		})

		It("waits, and neither marks nor promotes", func(ctx SpecContext) {
			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))

			Expect(acquirer.calls).To(Equal([]string{"Position"}))
			Expect(promoted).To(BeEmpty())
		})

		It("emits a warning event that says why the promotion is held back", func(ctx SpecContext) {
			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			Expect(recorder.Events).To(Receive(And(
				HavePrefix("Warning "+promotionHeldBackReason+" "),
				ContainSubstring("does not have all the data of the previous primary"),
				ContainSubstring("kubectl cnpg promote"),
			)))
		})

		It("emits the same event at every evaluation, for the recorder to count", func(ctx SpecContext) {
			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			_, err = r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			Expect(recorder.Events).To(HaveLen(2))
			Expect(<-recorder.Events).To(Equal(<-recorder.Events))
		})

		It("does not need an event recorder", func(ctx SpecContext) {
			r.recorder = nil

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
		})
	})

	When("the replica is on a later timeline than the previous primary", func() {
		BeforeEach(func() {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/7000028", 1)
		})

		It("is blocked: it neither marks nor promotes, and looks again later", func(ctx SpecContext) {
			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(promotionBlockedRetryPeriod))

			Expect(acquirer.calls).To(Equal([]string{"Position"}))
			Expect(promoted).To(BeEmpty())
			Expect(recorder.Events).To(Receive(And(
				HavePrefix("Warning "+promotionHeldBackReason+" "),
				ContainSubstring("kubectl annotate lease"),
			)))
		})
	})

	When("the replica waits for something that goes away", func() {
		It("emits an event that does not tell how to accept a loss, for an earlier timeline", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 3)

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(recorder.Events).To(Receive(And(
				HavePrefix("Warning "+promotionHeldBackReason+" "),
				ContainSubstring("earlier timeline"),
				Not(ContainSubstring("kubectl annotate lease")),
			)))
		})
	})

	It("promotes after a failover, which the gate only reports", func(ctx SpecContext) {
		acquirer.annotation = published(utils.PgDataStateInProduction, "0/9000000", 2)

		result, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
		Expect(recorder.Events).To(BeEmpty())
	})

	It("promotes during a failover, also when the previous primary stopped cleanly", func(ctx SpecContext) {
		acquirer.annotation = published(utils.PgDataStateShutDown, "0/9000028", 2)
		cluster.Status.Phase = apiv1.PhaseFailOver

		result, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
		Expect(recorder.Events).To(BeEmpty())
	})

	It("promotes when the lease has no position", func(ctx SpecContext) {
		result, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(promoted).To(HaveLen(1))
	})

	It("ignores the position of an earlier life of the cluster for the promotion of a replica cluster",
		func(ctx SpecContext) {
			// A position of an earlier life of this cluster, on an older timeline
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/9000028", 1)
			cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{PromotionToken: "token"}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
		})

	It("ignores the position of an earlier life of the cluster when this pod is the current primary already",
		func(ctx SpecContext) {
			// The designated primary of a replica cluster is promoted without
			// a token: no other primary is being replaced
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/9000028", 1)
			cluster.Status.CurrentPrimary = podName

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
			Expect(recorder.Events).To(BeEmpty())
		})

	It("does not promote when the promotion cannot be marked on the lease", func(ctx SpecContext) {
		acquirer.markErr = errors.New("the API server is on fire")

		_, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).To(MatchError(ContainSubstring("on fire")))
		Expect(promoted).To(BeEmpty())
	})

	When("the position of the previous primary cannot be read", func() {
		BeforeEach(func() {
			acquirer.positionErr = errors.New("the lease is held by someone else")
		})

		It("fails, and does not promote", func(ctx SpecContext) {
			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).To(MatchError(ContainSubstring("someone else")))
			Expect(promoted).To(BeEmpty())
		})
	})

	When("the position of this replica cannot be read", func() {
		BeforeEach(func() {
			r.readReplicaPosition = func(context.Context) (replicaPosition, error) {
				return replicaPosition{}, errors.New("connection refused")
			}
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
		})

		It("waits, and does not promote, after a clean shutdown", func(ctx SpecContext) {
			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(acquirer.calls).To(Equal([]string{"Position"}))
			Expect(promoted).To(BeEmpty())
		})

		It("does not hold back a failover, which it cannot check anyway", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateInProduction, "0/9000000", 2)

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(HaveLen(1))
		})
	})

	When("the timeline of the replica is earlier than the one of the previous primary", func() {
		var positions []replicaPosition
		var reads, checkpoints int

		BeforeEach(func() {
			positions = nil
			reads, checkpoints = 0, 0
			r.readReplicaPosition = func(context.Context) (replicaPosition, error) {
				position := positions[min(reads, len(positions)-1)]
				reads++
				return position, nil
			}
			r.checkpoint = func(context.Context) error {
				checkpoints++
				return nil
			}
		})

		It("requests a checkpoint after a clean shutdown, and decides on the position read after it", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 1}, {LSN: "0/80000A0", Timeline: 2}}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(HaveLen(1))
			Expect(checkpoints).To(Equal(1))
			Expect(reads).To(Equal(2))
		})

		It("does not request it when the replica is missing WAL, as it waits anyway", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/9000028", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 1}}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(checkpoints).To(BeZero())
			Expect(recorder.Events).To(Receive(And(
				ContainSubstring("does not have all the data"),
				ContainSubstring("kubectl annotate lease"),
			)))
		})

		It("waits when it is still earlier after the checkpoint", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 1}}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(checkpoints).To(Equal(1))
			Expect(promoted).To(BeEmpty())
		})

		It("waits with the position it has when the checkpoint fails", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 1}}
			r.checkpoint = func(context.Context) error {
				checkpoints++
				return errors.New("canceling statement due to statement timeout")
			}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(checkpoints).To(Equal(1))
			Expect(reads).To(Equal(1))
		})

		It("decides on the first position when it cannot be read again after the checkpoint", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
			r.readReplicaPosition = func(context.Context) (replicaPosition, error) {
				reads++
				if reads > 1 {
					return replicaPosition{}, errors.New("connection refused")
				}
				return replicaPosition{LSN: "0/80000A0", Timeline: 1}, nil
			}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(cluster.GetPrimaryLeaseRetryPeriod()))
			Expect(checkpoints).To(Equal(1))
			Expect(recorder.Events).To(Receive(ContainSubstring("earlier timeline")))
		})

		It("does not request it in a failover when the position cannot be read", func(ctx SpecContext) {
			cluster.Status.Phase = apiv1.PhaseFailOver
			acquirer.annotation = published(utils.PgDataStateInProduction, "0/9000000", 2)
			r.readReplicaPosition = func(context.Context) (replicaPosition, error) {
				return replicaPosition{}, errors.New("connection refused")
			}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(HaveLen(1))
			Expect(checkpoints).To(BeZero())
		})

		It("does not request it in a failover, which only reports, and promotes", func(ctx SpecContext) {
			cluster.Status.Phase = apiv1.PhaseFailOver
			acquirer.annotation = published(utils.PgDataStateInProduction, "0/9000000", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 1}, {LSN: "0/80000A0", Timeline: 2}}

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(HaveLen(1))
			Expect(checkpoints).To(BeZero())
			Expect(reads).To(Equal(1))
		})

		It("does not request it when the timeline is the one of the previous primary", func(ctx SpecContext) {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
			positions = []replicaPosition{{LSN: "0/80000A0", Timeline: 2}}

			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(checkpoints).To(BeZero())
			Expect(reads).To(Equal(1))
		})
	})
})
