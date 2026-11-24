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
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// resetFailoverQuorumObject resets the content of the sync quorum object
// to prevent unsafe failovers when we are changing the configuration
func (r *InstanceReconciler) resetFailoverQuorumObject(ctx context.Context, cluster *apiv1.Cluster) error {
	if !r.shouldManageFailoverQuorumObject(cluster) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var livingQuorumStatus apiv1.FailoverQuorum

		err := r.client.Get(ctx, client.ObjectKeyFromObject(cluster), &livingQuorumStatus)
		if err != nil {
			return err
		}

		livingQuorumStatus.Status = apiv1.FailoverQuorumStatus{}
		return r.client.Status().Update(ctx, &livingQuorumStatus)
	})
}

// failoverQuorumRequeueDelay is how long we wait before checking again if the
// standbys are aligned with the primary
const failoverQuorumRequeueDelay = time.Second

// updateFailoverQuorumObject updates the sync quorum object reading the
// current synchronous replica metadata from the PG instance.
//
// A status listing some standbys is published only when enough of them are
// aligned with the primary (see decideFailoverQuorumAction). Until then the
// object is left empty, so no failover is allowed.
//
// The returned boolean is true when the caller should schedule another
// reconciliation loop to check again.
func (r *InstanceReconciler) updateFailoverQuorumObject(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	if !r.shouldManageFailoverQuorumObject(cluster) {
		r.pendingFailoverQuorum = nil
		return false, nil
	}

	desired, err := r.getDesiredFailoverQuorumStatus(ctx)
	if err != nil {
		return false, err
	}

	published, err := r.getFailoverQuorumStatus(ctx, cluster)
	if err != nil {
		return false, err
	}

	// The loop runs a second time only after a Capture, which sets
	// a pending state matching the desired one.
	for {
		aligned := 0
		if r.pendingFailoverQuorum != nil &&
			equality.Semantic.DeepEqual(r.pendingFailoverQuorum.desired, desired) {
			aligned, err = r.instance.CountStandbysFlushedAtLeast(
				ctx, desired.StandbyNames, r.pendingFailoverQuorum.lsn)
			if err != nil {
				return false, err
			}
		}

		switch decideFailoverQuorumAction(desired, published, r.pendingFailoverQuorum, aligned) {
		case failoverQuorumActionNone:
			r.pendingFailoverQuorum = nil
			return false, nil

		case failoverQuorumActionPublish:
			if err := r.setFailoverQuorumStatus(ctx, cluster, desired); err != nil {
				return false, err
			}
			contextLogger.Info(
				"Failover quorum status published",
				"status", desired,
				"aligned", aligned,
			)
			r.pendingFailoverQuorum = nil
			return false, nil

		case failoverQuorumActionWait:
			contextLogger.Debug(
				"Waiting for the standbys to be aligned before publishing the failover quorum status",
				"lsn", r.pendingFailoverQuorum.lsn,
				"aligned", aligned,
				"required", desired.StandbyNumber,
			)
			return true, nil

		case failoverQuorumActionCapture:
			// The published status refers to a previous configuration, and
			// it can't be trusted anymore.
			if !equality.Semantic.DeepEqual(published, apiv1.FailoverQuorumStatus{}) {
				if err := r.setFailoverQuorumStatus(ctx, cluster, apiv1.FailoverQuorumStatus{}); err != nil {
					return false, err
				}
				published = apiv1.FailoverQuorumStatus{}
			}

			lsn, err := r.instance.GetCurrentWALInsertLSN(ctx)
			if err != nil {
				return false, err
			}
			contextLogger.Info(
				"Failover quorum status changed, waiting for the standbys to reach the current WAL position",
				"lsn", lsn,
				"desired", desired,
			)
			r.pendingFailoverQuorum = &pendingFailoverQuorum{desired: desired, lsn: lsn}
		}
	}
}

// getDesiredFailoverQuorumStatus computes the FailoverQuorum status
// from the synchronous replication configuration of the PostgreSQL instance
func (r *InstanceReconciler) getDesiredFailoverQuorumStatus(
	ctx context.Context,
) (apiv1.FailoverQuorumStatus, error) {
	metadata, err := r.Instance().GetSynchronousReplicationMetadata(ctx)
	if err != nil {
		return apiv1.FailoverQuorumStatus{}, err
	}

	desired := apiv1.FailoverQuorumStatus{}
	if metadata == nil {
		return desired, nil
	}

	desired.Method = metadata.Method
	desired.Primary = r.instance.GetPodName()
	desired.StandbyNumber = metadata.NumSync

	// We ensure the primary is not included in the standby names
	desired.StandbyNames = make([]string, 0, len(metadata.StandbyNames))
	for _, name := range metadata.StandbyNames {
		if name == desired.Primary {
			continue
		}
		desired.StandbyNames = append(desired.StandbyNames, name)
	}

	return desired, nil
}

// getFailoverQuorumStatus reads the status of the FailoverQuorum object
func (r *InstanceReconciler) getFailoverQuorumStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (apiv1.FailoverQuorumStatus, error) {
	var livingQuorumStatus apiv1.FailoverQuorum
	if err := r.client.Get(ctx, client.ObjectKeyFromObject(cluster), &livingQuorumStatus); err != nil {
		return apiv1.FailoverQuorumStatus{}, err
	}

	return livingQuorumStatus.Status, nil
}

// setFailoverQuorumStatus sets the status of the FailoverQuorum object,
// if it differs from the passed one
func (r *InstanceReconciler) setFailoverQuorumStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	status apiv1.FailoverQuorumStatus,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var livingQuorumStatus apiv1.FailoverQuorum

		err := r.client.Get(ctx, client.ObjectKeyFromObject(cluster), &livingQuorumStatus)
		if err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(livingQuorumStatus.Status, status) {
			return nil
		}

		updatedQuorumStatus := livingQuorumStatus.DeepCopy()
		updatedQuorumStatus.Status = status
		return r.client.Status().Update(ctx, updatedQuorumStatus)
	})
}

// pendingFailoverQuorum tracks a FailoverQuorum status that has been computed
// from the PostgreSQL configuration but that has not been published yet,
// because we are still waiting for the standbys to be aligned with the primary.
type pendingFailoverQuorum struct {
	// desired is the status that will be published when the standbys are aligned
	desired apiv1.FailoverQuorumStatus

	// lsn is the WAL insert position of the primary, taken after the
	// configuration backing `desired` became effective. Every transaction
	// acknowledged under a previous configuration has its commit record
	// before this position.
	lsn string
}

// failoverQuorumAction is the decision taken by decideFailoverQuorumAction
type failoverQuorumAction int

const (
	// failoverQuorumActionNone means that the published status is already
	// the desired one
	failoverQuorumActionNone failoverQuorumAction = iota

	// failoverQuorumActionCapture means that a change has just been detected
	// (or the desired status changed while waiting): the caller must reset
	// the published status, record the current WAL insert LSN as the new
	// pending state and evaluate the decision again.
	failoverQuorumActionCapture

	// failoverQuorumActionWait means that not enough standbys reached the
	// pending LSN yet: the published status must be left untouched (empty)
	// and a new reconciliation loop requested.
	failoverQuorumActionWait

	// failoverQuorumActionPublish means that the desired status is safe to
	// be published
	failoverQuorumActionPublish
)

// decideFailoverQuorumAction decides what to do with the FailoverQuorum
// status given the desired and the published ones.
//
// Publishing a status listing a set S of standbys with a number k tells the
// operator that every acknowledged transaction is present on at least k
// members of S. That holds for the transactions committed after the new
// synchronous replication configuration became effective, but the ones
// acknowledged before it were acknowledged by a different set of standbys.
// The status is thus published only when at least k members of S have flushed
// the WAL up to the position taken when the change was detected.
//
// The `aligned` parameter is the number of members of `desired.StandbyNames`
// that have flushed `pending.lsn`. It is ignored when there is no pending
// state matching the desired one.
func decideFailoverQuorumAction(
	desired apiv1.FailoverQuorumStatus,
	published apiv1.FailoverQuorumStatus,
	pending *pendingFailoverQuorum,
	aligned int,
) failoverQuorumAction {
	if equality.Semantic.DeepEqual(desired, published) {
		return failoverQuorumActionNone
	}

	// Nothing is promised by an empty status, so there is nothing to wait for
	if equality.Semantic.DeepEqual(desired, apiv1.FailoverQuorumStatus{}) {
		return failoverQuorumActionPublish
	}

	if pending == nil || !equality.Semantic.DeepEqual(pending.desired, desired) {
		return failoverQuorumActionCapture
	}

	if aligned >= desired.StandbyNumber {
		return failoverQuorumActionPublish
	}

	return failoverQuorumActionWait
}

func (r *InstanceReconciler) shouldManageFailoverQuorumObject(cluster *apiv1.Cluster) bool {
	if cluster.Status.TargetPrimary != r.instance.GetPodName() {
		return false
	}
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		return false
	}
	if cluster.Spec.PostgresConfiguration.Synchronous == nil {
		return false
	}

	return cluster.IsFailoverQuorumActive()
}
