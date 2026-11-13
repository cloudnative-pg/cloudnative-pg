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
			enforcePromotionGate: true,
			recorder:             recorder,
			readReplicaPosition: func(context.Context) (replicaPosition, error) {
				return replicaPosition{LSN: "0/80000A0", Timeline: 2}, nil
			},
			promoteInstance: func(context.Context) error {
				promoted = append(promoted, append([]string(nil), acquirer.calls...))
				return nil
			},
		}

		// This pod is the current primary already, so that the promotion
		// does not wait for the WAL receiver through a database connection
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Status:     apiv1.ClusterStatus{TargetPrimary: podName, CurrentPrimary: podName},
		}
	})

	When("the replica has all the data of the previous primary", func() {
		BeforeEach(func() {
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
		})

		It("marks the promotion with the position of the replica, and then promotes", func(ctx SpecContext) {
			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())

			Expect(acquirer.markedLSN).To(Equal("0/80000A0"))
			Expect(acquirer.markedTimeline).To(Equal(2))
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

		It("only logs the decision while the gate is not enforced, and promotes", func(ctx SpecContext) {
			r.enforcePromotionGate = false

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(Equal([][]string{{"Position", "MarkPromoting"}}))
			Expect(recorder.Events).To(BeEmpty())
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
			Expect(recorder.Events).To(Receive(HavePrefix("Warning " + promotionHeldBackReason + " ")))
		})

		It("only logs the decision while the gate is not enforced, and promotes", func(ctx SpecContext) {
			r.enforcePromotionGate = false

			result, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.IsZero()).To(BeTrue())
			Expect(promoted).To(HaveLen(1))
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

	It("promotes when the lease has no position", func(ctx SpecContext) {
		result, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(promoted).To(HaveLen(1))
	})

	It("does not run the gate for the promotion of a replica cluster, but still marks it", func(ctx SpecContext) {
		// A position of an earlier life of this cluster, on an older timeline
		acquirer.annotation = published(utils.PgDataStateShutDown, "0/7000028", 1)
		cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{PromotionToken: "token"}

		result, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
		Expect(promoted).To(Equal([][]string{{"MarkPromoting"}}))
	})

	It("does not promote when the promotion cannot be marked on the lease", func(ctx SpecContext) {
		acquirer.markErr = errors.New("the API server is on fire")

		_, err := r.promoteAfterGate(ctx, cluster)
		Expect(err).To(MatchError(ContainSubstring("on fire")))
		Expect(promoted).To(BeEmpty())
	})

	It("does not promote when the promotion cannot be marked, even if the gate is not enforced",
		func(ctx SpecContext) {
			r.enforcePromotionGate = false
			acquirer.markErr = errors.New("the API server is on fire")

			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(promoted).To(BeEmpty())
		})

	When("the position of the previous primary cannot be read", func() {
		BeforeEach(func() {
			acquirer.positionErr = errors.New("the lease is held by someone else")
		})

		It("fails, and does not promote, when the gate is enforced", func(ctx SpecContext) {
			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).To(MatchError(ContainSubstring("someone else")))
			Expect(promoted).To(BeEmpty())
		})

		It("skips the gate when it is not enforced", func(ctx SpecContext) {
			r.enforcePromotionGate = false

			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(promoted).To(HaveLen(1))
		})
	})

	When("the position of this replica cannot be read", func() {
		BeforeEach(func() {
			r.readReplicaPosition = func(context.Context) (replicaPosition, error) {
				return replicaPosition{}, errors.New("connection refused")
			}
			acquirer.annotation = published(utils.PgDataStateShutDown, "0/8000028", 2)
		})

		It("waits, and does not promote, after a clean shutdown when the gate is enforced", func(ctx SpecContext) {
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

		It("goes on without it when the gate is not enforced", func(ctx SpecContext) {
			r.enforcePromotionGate = false

			_, err := r.promoteAfterGate(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(acquirer.markedLSN).To(BeEmpty())
			Expect(promoted).To(HaveLen(1))
		})
	})
})

var _ = Describe("promotionDecision.logMessage", func() {
	DescribeTable("says what happens to the promotion",
		func(verdict promotionVerdict, enforced bool, suffix string) {
			message := promotionDecision{Verdict: verdict, Message: "The decision"}.logMessage(enforced)
			if suffix == "" {
				Expect(message).To(Equal("The decision"))
			} else {
				Expect(message).To(Equal("The decision" + suffix))
			}
		},
		Entry("a promotion that proceeds, enforced or not", promotionProceed, false, ""),
		Entry("a promotion that proceeds, enforced", promotionProceed, true, ""),
		Entry("a wait that is enforced", promotionWait, true, ""),
		Entry("a block that is enforced", promotionBlocked, true, ""),
		Entry("a wait that is not enforced", promotionWait, false,
			" (the promotion gate is not enforced, the promotion goes on)"),
		Entry("a block that is not enforced", promotionBlocked, false,
			" (the promotion gate is not enforced, the promotion goes on)"),
	)
})

var _ = Describe("the stage of the promotion gate", func() {
	It("is enforced for the instance reconcilers of the instance manager", func() {
		r := NewInstanceReconciler(postgres.NewInstance(), nil, nil, nil, &fakeLeaseAcquirer{}, nil, nil)
		Expect(r.enforcePromotionGate).To(BeTrue())
	})
})
