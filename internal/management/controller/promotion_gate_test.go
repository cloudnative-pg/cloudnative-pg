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
			Annotation: annotation,
			PodName:    thisPod,
			Replica:    replica,
		})
	}

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
		Entry("another pod is promoting",
			position(utils.LeasePositionStatePromoting, "0/70000A0", 2),
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

	It("names the pod that is promoting when it is not this one", func() {
		decision := decide(position(utils.LeasePositionStatePromoting, "0/70000A0", 2),
			replica("0/70000A0", 2))
		Expect(decision.Details).To(ContainElements("leasePosition", utils.LeasePosition{
			LSN: "0/70000A0", Timeline: 2, State: utils.LeasePositionStatePromoting, Pod: previousPrimary,
		}))
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
		// The lease holds the start of the shutdown checkpoint, which is
		// 120 bytes long: a replica that has everything is at its end
		Entry("the replica has everything",
			"0/7000028", 1, "0/70000A0", 1, promotionProceed, promotionLogInfo),
		Entry("the replica has everything, on the next switchover",
			"0/8000028", 2, "0/80000A0", 2, promotionProceed, promotionLogInfo),
		Entry("the replica is exactly at the position of the lease",
			"0/7000028", 1, "0/7000028", 1, promotionProceed, promotionLogInfo),
		Entry("the replica is ahead of the position of the lease",
			"0/7000028", 1, "0/7004FA0", 1, promotionProceed, promotionLogInfo),
		Entry("the replica is a byte behind the lease",
			"0/7000028", 1, "0/7000027", 1, promotionWait, promotionLogWarning),
		Entry("the replica is behind the lease across a segment boundary",
			"1/00000028", 1, "0/FFFFFFF0", 1, promotionWait, promotionLogWarning),
		Entry("the replica is behind the lease",
			"0/8000028", 2, "0/7004FA0", 2, promotionWait, promotionLogWarning),
		Entry("the replica is on an earlier timeline",
			"0/8000028", 2, "0/70000A0", 1, promotionWait, promotionLogInfo),
		Entry("the replica is on an earlier timeline, with a higher LSN",
			"0/8000028", 2, "0/9000000", 1, promotionWait, promotionLogInfo),
		Entry("the replica is on a later timeline",
			"0/7000028", 1, "0/70000A0", 2, promotionBlocked, promotionLogError),
		Entry("the replica is on a later timeline, with a lower LSN",
			"0/8000028", 2, "0/1000000", 3, promotionBlocked, promotionLogError),
		Entry("the position of the replica is not available",
			"0/7000028", 1, "", 0, promotionWait, promotionLogWarning),
		Entry("the timeline of the replica is not available",
			"0/7000028", 1, "0/70000A0", 0, promotionWait, promotionLogWarning),
	)

	It("tells how much is missing when the replica is behind, and how to go on without it", func() {
		decision := decide(position(utils.PgDataStateShutDown, "0/7000028", 1),
			replica("0/7000000", 1))
		Expect(decision.Verdict).To(Equal(promotionWait))
		Expect(decision.Details).To(ContainElements("missingBytes", uint64(0x28)))
		Expect(decision.Message).To(ContainSubstring("kubectl annotate lease <cluster> cnpg.io/position-"))
	})

	It("tells how to unblock a replica that is blocked", func() {
		decision := decide(position(utils.PgDataStateShutDown, "0/7000028", 1),
			replica("0/70000A0", 2))
		Expect(decision.Verdict).To(Equal(promotionBlocked))
		Expect(decision.Message).To(ContainSubstring("kubectl annotate lease <cluster> cnpg.io/position-"))
		Expect(decision.Message).To(ContainSubstring("kubectl cnpg promote"))
	})

	Context("when the previous primary did not stop cleanly", func() {
		DescribeTable("always proceeds, reporting what may be missing",
			func(state utils.PgDataState) {
				decision := decide(position(state, "0/8000000", 2), replica("0/7FFFF00", 2))
				Expect(decision.Verdict).To(Equal(promotionProceed))
				Expect(decision.Level).To(Equal(promotionLogWarning))
				Expect(decision.Details).To(ContainElements(
					"leasePosition", utils.LeasePosition{
						LSN: "0/8000000", Timeline: 2, State: state, Pod: previousPrimary,
					},
					"possiblyMissingBytes", uint64(0x100),
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
	})
})
