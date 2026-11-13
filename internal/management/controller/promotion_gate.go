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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// promotionVerdict is what the promotion gate decides for a replica that is
// about to promote
type promotionVerdict string

const (
	// promotionProceed means that the replica can promote
	promotionProceed promotionVerdict = "proceed"

	// promotionWait means that the replica must not promote yet, and is
	// evaluated again later, as it may catch up
	promotionWait promotionVerdict = "wait"

	// promotionBlocked means that the replica must not promote, and
	// evaluating it again will not change that. A person has to decide.
	promotionBlocked promotionVerdict = "blocked"
)

// promotionLogLevel is the level at which a decision of the gate is logged
type promotionLogLevel string

const (
	promotionLogInfo    promotionLogLevel = "info"
	promotionLogWarning promotionLogLevel = "warning"
	promotionLogError   promotionLogLevel = "error"
)

// replicaPosition is how far a replica is in the WAL of its timeline
type replicaPosition struct {
	// LSN is the end of the WAL that the replica has, received or replayed
	LSN types.LSN

	// Timeline is the timeline the replica is on. Zero if not known.
	Timeline int
}

// promotionGateInput is what the promotion gate decides on
type promotionGateInput struct {
	// Annotation is the value of the position annotation of the primary lease.
	// Empty when the lease does not have it, or has it empty: both mean that no
	// position was published, which is also how a person accepts a loss.
	Annotation string

	// PodName is the pod that is about to promote
	PodName string

	// Replica is the position of the pod that is about to promote
	Replica replicaPosition
}

// promotionDecision is the result of the promotion gate, with what to log
// about it
type promotionDecision struct {
	Verdict promotionVerdict
	Level   promotionLogLevel
	Message string

	// Details are the key/value pairs to log with the message
	Details []any
}

// decidePromotion tells whether a replica can promote, given the position that
// the previous primary published on the primary lease.
//
// Only a clean shutdown publishes a position that is final, so it is the only
// case that can hold a replica back: the data is known to exist, and the
// replica is either missing some of it or is on a timeline that is not the one
// the previous primary stopped on. Everything else is a failover, or a position
// that is not known, and the replica proceeds: it is reported, not prevented.
func decidePromotion(input promotionGateInput) promotionDecision {
	if input.Annotation == "" {
		return promotionDecision{
			Verdict: promotionProceed,
			Level:   promotionLogInfo,
			Message: "The primary lease has no position of the previous primary, proceeding with the promotion",
		}
	}

	leasePosition, err := utils.ParseLeasePosition(input.Annotation)
	if err != nil {
		return promotionDecision{
			Verdict: promotionProceed,
			Level:   promotionLogWarning,
			Message: "The position of the previous primary on the primary lease cannot be understood, " +
				"proceeding with the promotion",
			Details: []any{"value", input.Annotation, "err", err.Error()},
		}
	}

	switch leasePosition.State {
	case utils.LeasePositionStateUnknown:
		return promotionDecision{
			Verdict: promotionProceed,
			Level:   promotionLogWarning,
			Message: "The position of the previous primary is not known, " +
				"proceeding with the promotion without being able to check it",
			Details: []any{"leasePosition", *leasePosition},
		}

	case utils.LeasePositionStatePromoting:
		if leasePosition.Pod == input.PodName {
			return promotionDecision{
				Verdict: promotionProceed,
				Level:   promotionLogInfo,
				Message: "This pod already passed the promotion gate, proceeding with the promotion",
				Details: []any{"leasePosition", *leasePosition},
			}
		}
		return promotionDecision{
			Verdict: promotionProceed,
			Level:   promotionLogWarning,
			Message: "Another pod started to promote and did not complete it, proceeding with the promotion",
			Details: []any{"leasePosition", *leasePosition},
		}

	case utils.PgDataStateShutDown:
		return decideAfterCleanShutdown(input, leasePosition)
	}

	return decideAfterFailover(input, leasePosition)
}

// decideAfterCleanShutdown is the decision of the gate when the previous
// primary published a final position
func decideAfterCleanShutdown(input promotionGateInput, leasePosition *utils.LeasePosition) promotionDecision {
	details := []any{
		"leasePosition", *leasePosition,
		"replicaLSN", input.Replica.LSN,
		"replicaTimeline", input.Replica.Timeline,
	}

	leaseLSN, err := types.LSN(leasePosition.LSN).Parse()
	if err != nil || leasePosition.Timeline == 0 {
		return promotionDecision{
			Verdict: promotionProceed,
			Level:   promotionLogWarning,
			Message: "The position of the previous primary on the primary lease is incomplete, " +
				"proceeding with the promotion without being able to check it",
			Details: details,
		}
	}

	replicaLSN, err := input.Replica.LSN.Parse()
	if err != nil || input.Replica.Timeline == 0 {
		return promotionDecision{
			Verdict: promotionWait,
			Level:   promotionLogWarning,
			Message: "The position of this replica is not available, waiting before checking " +
				"that it has all the data of the previous primary",
			Details: details,
		}
	}

	switch {
	case input.Replica.Timeline < leasePosition.Timeline:
		return promotionDecision{
			Verdict: promotionWait,
			Level:   promotionLogInfo,
			Message: "This replica is on an earlier timeline than the one the previous primary stopped on, " +
				"waiting for it to catch up",
			Details: details,
		}

	case input.Replica.Timeline > leasePosition.Timeline:
		return promotionDecision{
			Verdict: promotionBlocked,
			Level:   promotionLogError,
			Message: "This replica is on a later timeline than the one the previous primary stopped on, " +
				"it may have diverged, not promoting it. " + unblockPromotionHint,
			Details: details,
		}

	case leaseLSN > replicaLSN:
		return promotionDecision{
			Verdict: promotionWait,
			Level:   promotionLogWarning,
			Message: "This replica does not have all the data of the previous primary, which exists only " +
				"there, waiting for the missing WAL to be delivered by the archive. " + unblockPromotionHint,
			Details: append(details, "missingBytes", leaseLSN-replicaLSN),
		}
	}

	return promotionDecision{
		Verdict: promotionProceed,
		Level:   promotionLogInfo,
		Message: "This replica has all the data of the previous primary, proceeding with the promotion",
		Details: details,
	}
}

// decideAfterFailover is the decision of the gate when the previous primary did
// not publish a final position, which is what a failover looks like. The
// replica promotes, and what is known about the data it may be missing is
// reported.
func decideAfterFailover(input promotionGateInput, leasePosition *utils.LeasePosition) promotionDecision {
	details := []any{
		"leasePosition", *leasePosition,
		"replicaLSN", input.Replica.LSN,
		"replicaTimeline", input.Replica.Timeline,
	}

	message := "The previous primary did not stop cleanly, proceeding with the promotion"

	// The positions can be compared only on the same timeline. The difference
	// is a lower bound: the previous primary may have written for up to about
	// a renewal period after the position was published.
	leaseLSN, leaseErr := types.LSN(leasePosition.LSN).Parse()
	replicaLSN, replicaErr := input.Replica.LSN.Parse()
	if leaseErr == nil && replicaErr == nil &&
		leasePosition.Timeline != 0 && leasePosition.Timeline == input.Replica.Timeline {
		missing := uint64(0)
		if leaseLSN > replicaLSN {
			missing = leaseLSN - replicaLSN
		}
		details = append(details, "possiblyMissingBytes", missing)
		message = fmt.Sprintf("%s, at least %d bytes of WAL may not be on this replica", message, missing)
	}

	return promotionDecision{
		Verdict: promotionProceed,
		Level:   promotionLogWarning,
		Message: message,
		Details: details,
	}
}

// unblockPromotionHint is what a person can do when the gate blocks a promotion
const unblockPromotionHint = "To promote this replica anyway, accepting the loss of the data that only the " +
	"previous primary has, remove the annotation of the primary lease: " +
	"kubectl annotate lease <cluster> " + utils.LeasePositionAnnotationName + "-. " +
	"To avoid the loss, promote the previous primary back: kubectl cnpg promote --force <cluster> <previous primary>"
