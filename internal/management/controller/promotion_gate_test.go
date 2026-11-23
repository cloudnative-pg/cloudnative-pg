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
	"time"

	"github.com/cloudnative-pg/machinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("decidePromotion", func() {
	const (
		thisPod         = "cluster-example-3"
		previousPrimary = "cluster-example-2"
	)

	positionBy := func(pod string, state utils.PgDataState, lsn string, timeline int) string {
		value, err := utils.LeasePosition{LSN: lsn, Timeline: timeline, State: state, Pod: pod}.Marshal()
		Expect(err).ToNot(HaveOccurred())
		return value
	}

	// position is what the previous primary published
	position := func(state utils.PgDataState, lsn string, timeline int) string {
		return positionBy(previousPrimary, state, lsn, timeline)
	}

	replica := func(lsn string, timeline int) replicaPosition {
		return replicaPosition{LSN: types.LSN(lsn), Timeline: timeline}
	}

	decide := func(annotation string, replica replicaPosition) promotionDecision {
		return decidePromotion(promotionGateInput{
			Annotation:    annotation,
			PodName:       thisPod,
			Replica:       replica,
			AnnotationAge: 3 * time.Second,
		})
	}

	It("proceeds when the cluster leaves the replica cluster mode, whatever the lease says", func() {
		// The annotation was left when the cluster was demoted, on a timeline
		// that the pod has left behind since
		decision := decidePromotion(promotionGateInput{
			Annotation:              position(utils.PgDataStateShutDown, "0/70000A0", 2),
			PodName:                 thisPod,
			Replica:                 replica("0/50000A0", 5),
			ReplicaClusterPromotion: true,
		})

		Expect(decision.Verdict).To(Equal(promotionProceed))
		Expect(decision.Level).To(Equal(promotionLogInfo))
	})

	It("blocks the same replica when the cluster is not leaving the replica cluster mode", func() {
		Expect(decide(position(utils.PgDataStateShutDown, "0/70000A0", 2), replica("0/50000A0", 5)).Verdict).
			To(Equal(promotionBlocked))
	})

	DescribeTable("when the previous primary did not publish a position that can be checked",
		func(annotation string, verdict promotionVerdict, level promotionLogLevel) {
			decision := decide(annotation, replica("0/70000A0", 2))
			Expect(decision.Verdict).To(Equal(verdict))
			Expect(decision.Level).To(Equal(level))
			Expect(decision.Message).ToNot(BeEmpty())
		},
		Entry("there is no annotation, or it is empty", "", promotionProceed, promotionLogInfo),
		Entry("the annotation is not JSON", "0/7000028", promotionProceed, promotionLogWarning),
		Entry("the state is not one we know", `{"state":"hibernating"}`,
			promotionProceed, promotionLogWarning),
		Entry("the position is unknown",
			position(utils.LeasePositionStateUnknown, "", 0),
			promotionProceed, promotionLogWarning),
		Entry("this pod is already promoting",
			positionBy(thisPod, utils.LeasePositionStatePromoting, "0/70000A0", 2),
			promotionProceed, promotionLogInfo),
		Entry("another pod is promoting, and left no position",
			position(utils.LeasePositionStatePromoting, "", 0),
			promotionProceed, promotionLogWarning),
		Entry("a clean shutdown is published without an LSN",
			position(utils.PgDataStateShutDown, "", 2),
			promotionProceed, promotionLogWarning),
		Entry("a clean shutdown is published without a timeline",
			position(utils.PgDataStateShutDown, "0/7000028", 0),
			promotionProceed, promotionLogWarning),
		Entry("a clean shutdown is published with an LSN that is not one",
			position(utils.PgDataStateShutDown, "garbage", 2),
			promotionProceed, promotionLogWarning),
	)

	Describe("when another pod started to promote and left the final position of the previous primary", func() {
		marker := func(lsn string, timeline int) string {
			return positionBy(previousPrimary, utils.LeasePositionStatePromoting, lsn, timeline)
		}

		It("makes a replica on that timeline wait for the data it is missing", func() {
			decision := decide(marker("0/70000A0", 2), replica("0/6000000", 2))
			Expect(decision.Verdict).To(Equal(promotionWait))
		})

		It("lets a replica on that timeline that has all the data promote", func() {
			decision := decide(marker("0/70000A0", 2), replica("0/70000A0", 2))
			Expect(decision.Verdict).To(Equal(promotionProceed))
		})

		It("does not block a replica on a later timeline, as the promotion happened", func() {
			decision := decide(marker("0/70000A0", 2), replica("0/6000000", 3))
			Expect(decision.Verdict).To(Equal(promotionProceed))
			Expect(decision.Level).To(Equal(promotionLogWarning))
		})
	})

	It("names the pod that is promoting when it is not this one", func() {
		decision := decide(position(utils.LeasePositionStatePromoting, "0/70000A0", 2),
			replica("0/70000A0", 3))
		Expect(decision.Details).To(ContainElements("writer", "cluster-example-2", "leaseLSN", "0/70000A0"))
	})

	DescribeTable("after a clean shutdown of the previous primary",
		func(leaseLSN string, leaseTimeline int, replicaLSN string, replicaTimeline int,
			verdict promotionVerdict, level promotionLogLevel,
		) {
			decision := decide(
				position(utils.PgDataStateShutDown, leaseLSN, leaseTimeline),
				replica(replicaLSN, replicaTimeline),
			)
			Expect(decision.Verdict).To(Equal(verdict))
			Expect(decision.Level).To(Equal(level))
		},
		// The lease holds where the WAL before the shutdown checkpoint ends,
		// the start of the segment that holds it: a replica that streamed has
		// its page header and the 120 bytes of the checkpoint too, one that
		// restored the WAL from the archive does not
		Entry("the replica has everything",
			"0/7000000", 1, "0/70000A0", 1, promotionProceed, promotionLogInfo),
		Entry("the replica has everything, on the next switchover",
			"0/8000000", 2, "0/80000A0", 2, promotionProceed, promotionLogInfo),
		Entry("the replica restored all the WAL from the archive",
			"0/7000000", 1, "0/7000000", 1, promotionProceed, promotionLogInfo),
		Entry("the replica is ahead of the position of the lease",
			"0/7000000", 1, "0/7004FA0", 1, promotionProceed, promotionLogInfo),
		Entry("the replica is a byte behind the lease",
			"0/7000000", 1, "0/6FFFFFF", 1, promotionWait, promotionLogWarning),
		Entry("the replica is behind the lease across a segment boundary",
			"1/00000000", 1, "0/FFFFFFF0", 1, promotionWait, promotionLogWarning),
		Entry("the replica is behind the lease",
			"0/8000000", 2, "0/7004FA0", 2, promotionWait, promotionLogWarning),
		Entry("the replica is on an earlier timeline",
			"0/8000000", 2, "0/70000A0", 1, promotionWait, promotionLogInfo),
		Entry("the replica is on an earlier timeline, with a higher LSN",
			"0/8000000", 2, "0/9000000", 1, promotionWait, promotionLogInfo),
		Entry("the replica is on a later timeline",
			"0/7000000", 1, "0/70000A0", 2, promotionBlocked, promotionLogError),
		Entry("the replica is on a later timeline, with a lower LSN",
			"0/8000000", 2, "0/1000000", 3, promotionBlocked, promotionLogError),
		Entry("the position of the replica is not available",
			"0/7000000", 1, "", 0, promotionWait, promotionLogWarning),
		Entry("the timeline of the replica is not available",
			"0/7000000", 1, "0/70000A0", 0, promotionWait, promotionLogWarning),
	)

	It("tells how much is missing when the replica is behind", func() {
		decision := decide(position(utils.PgDataStateShutDown, "0/7000000", 1),
			replica("0/6FFFF00", 1))
		Expect(decision.Verdict).To(Equal(promotionWait))
		Expect(decision.Details).To(ContainElements("missingBytes", uint64(0x100)))
	})

	It("asks to tell how to unblock a replica that is blocked", func() {
		decision := decide(position(utils.PgDataStateShutDown, "0/7000000", 1),
			replica("0/70000A0", 2))
		Expect(decision.Verdict).To(Equal(promotionBlocked))
		Expect(decision.Unblock).To(BeTrue())
	})

	It("asks to tell how to unblock a replica that is missing WAL", func() {
		decision := decide(position(utils.PgDataStateShutDown, "0/7000000", 1),
			replica("0/6FFFF00", 1))
		Expect(decision.Verdict).To(Equal(promotionWait))
		Expect(decision.Unblock).To(BeTrue())
	})

	DescribeTable("does not ask to tell how to unblock a replica that waits for something that goes away",
		func(replica replicaPosition) {
			decision := decide(position(utils.PgDataStateShutDown, "0/7000000", 2), replica)
			Expect(decision.Verdict).To(Equal(promotionWait))
			Expect(decision.Unblock).To(BeFalse())
		},
		Entry("an earlier timeline", replicaPosition{LSN: "0/70000A0", Timeline: 1}),
		Entry("a position that is not available", replicaPosition{}),
	)

	Context("when the previous primary did not stop cleanly", func() {
		DescribeTable("always proceeds, reporting what may be missing",
			func(state utils.PgDataState) {
				decision := decide(position(state, "0/8000000", 2), replica("0/7FFFF00", 2))
				Expect(decision.Verdict).To(Equal(promotionProceed))
				Expect(decision.Level).To(Equal(promotionLogWarning))
				Expect(decision.Details).To(ContainElements(
					"writer", "cluster-example-2",
					"state", state,
					"possiblyMissingBytes", uint64(0x100),
					"age", "3s",
				))
			},
			Entry("in production", utils.PgDataStateInProduction),
			Entry("shutting down", utils.PgDataStateShuttingDown),
			Entry("in crash recovery", utils.PgDataStateInCrashRecovery),
		)

		It("does not report a negative amount when the replica is ahead", func() {
			decision := decide(position(utils.PgDataStateInProduction, "0/8000000", 2),
				replica("0/9000000", 2))
			Expect(decision.Verdict).To(Equal(promotionProceed))
			Expect(decision.Details).To(ContainElements("possiblyMissingBytes", uint64(0)))
		})

		It("does not compare the positions of different timelines", func() {
			decision := decide(position(utils.PgDataStateInProduction, "0/8000000", 2),
				replica("0/7000000", 1))
			Expect(decision.Verdict).To(Equal(promotionProceed))
			Expect(decision.Details).ToNot(ContainElement("possiblyMissingBytes"))
		})

		It("proceeds when the position of the replica is not available", func() {
			decision := decide(position(utils.PgDataStateInProduction, "0/8000000", 2),
				replicaPosition{})
			Expect(decision.Verdict).To(Equal(promotionProceed))
			Expect(decision.Details).ToNot(ContainElement("possiblyMissingBytes"))
		})

		It("leaves the age out when it is not known", func() {
			decision := decidePromotion(promotionGateInput{
				Annotation: position(utils.PgDataStateInProduction, "0/8000000", 2),
				PodName:    thisPod,
				Replica:    replica("0/7FFFF00", 2),
			})
			Expect(decision.Details).ToNot(ContainElement("age"))
		})
	})

	Context("when the operator is failing over", func() {
		failover := func(annotation string, replica replicaPosition) promotionDecision {
			return decidePromotion(promotionGateInput{
				Annotation: annotation,
				PodName:    thisPod,
				Replica:    replica,
				Failover:   true,
			})
		}

		DescribeTable("never holds the replica back, also after a clean shutdown of the previous primary",
			func(annotation string, replica replicaPosition) {
				decision := failover(annotation, replica)
				Expect(decision.Verdict).To(Equal(promotionProceed))
				Expect(decision.Level).To(Equal(promotionLogWarning))
			},
			Entry("the replica is behind the lease",
				position(utils.PgDataStateShutDown, "0/8000000", 2), replica("0/7FFFF00", 2)),
			Entry("the replica is on an earlier timeline",
				position(utils.PgDataStateShutDown, "0/8000000", 2), replica("0/7000000", 1)),
			Entry("the replica is on a later timeline",
				position(utils.PgDataStateShutDown, "0/8000000", 2), replica("0/7000000", 3)),
			Entry("the position of the replica is not available",
				position(utils.PgDataStateShutDown, "0/8000000", 2), replicaPosition{}),
			Entry("another pod started to promote and left the final position of the previous primary",
				position(utils.LeasePositionStatePromoting, "0/8000000", 2), replica("0/7FFFF00", 2)),
		)

		It("reports what the replica is missing, and that the previous primary stopped cleanly", func() {
			decision := failover(position(utils.PgDataStateShutDown, "0/8000000", 2), replica("0/7FFFF00", 2))
			Expect(decision.Details).To(ContainElements("possiblyMissingBytes", uint64(0x100)))
			Expect(decision.Message).To(ContainSubstring("stopped cleanly"))
			Expect(decision.Message).ToNot(ContainSubstring("did not stop cleanly"))
		})
	})
})
