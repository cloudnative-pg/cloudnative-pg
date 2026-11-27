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

// failoverQuorumCheckpointTimeout is how long a reconciliation loop waits for
// the checkpoint requested to make the synchronous standbys effective. The
// checkpoint is not interrupted when the timeout expires: it is waited for in
// the following loops, without requesting it again.
const failoverQuorumCheckpointTimeout = 15 * time.Second

// updateFailoverQuorumObject keeps the FailoverQuorum object in line with the
// synchronous replication configuration of the PostgreSQL instance. It runs in
// the primary, at every reconciliation loop, after the configuration has been
// loaded.
//
// # The invariant
//
// A published status lists some standbys and a number k. It tells the operator
// that every transaction acknowledged to a client is present on at least k of
// the listed standbys, and the operator relies on it to decide that a failover
// can't lose acknowledged transactions. An empty status promises nothing, and
// the operator refuses any failover.
//
// # The protocol
//
// The status is not published as soon as the configuration changes, but only
// after these steps:
//
//  1. Reset. The object is emptied before the configuration is reloaded,
//     because while the new configuration is being applied we can't tell
//     which standbys the transactions wait for. This is done by the
//     reconciliation loop right before the reload. It is emptied again when
//     a change is detected and we start waiting (captureFailoverQuorum), to
//     not leave a status that refers to the previous configuration.
//  2. Reload. PostgreSQL loads the new configuration, and the reconciliation
//     loop waits for that to happen.
//  3. Checkpoint. Loading the configuration is not enough to make committing
//     backends wait for the standbys (see ensureSyncStandbysEffective), so a
//     checkpoint is requested to make sure they do. This is skipped when we
//     already know it is the case (syncStandbysEffective), and the request is
//     made once, even if the checkpoint takes long (checkpointRequestedAtLSN).
//  4. Align. The current WAL insert position is read, and we wait until at
//     least NumSync of the listed standbys have flushed it. Not all of them:
//     k is what the invariant needs. While waiting, the object stays empty
//     and the caller is asked to run another loop.
//  5. Publish. The status is written.
//
// # Why each step exists
//
// Steps 1 and 5 bracket the period in which the invariant is not known to
// hold. Without them the operator could trust a status describing a
// configuration that is not in force.
//
// Step 4 exists because the transactions acknowledged under the previous
// configuration were acknowledged by a different set of standbys. A standby
// that is catching up, for example with a newly promoted primary, lacks some
// of them, and promoting it would lose them. Once at least k of the listed
// standbys have the WAL up to a given position, every transaction committed
// before it is on at least k of them. The transactions committed after it
// are on k of them because they wait for the standbys.
//
// That last statement is true only when the position is read after step 3.
// Until the checkpointer has applied the configuration, committing backends
// return without waiting for the standbys, even if they are listed. The
// commits that returned that way can be anywhere in the WAL before that
// moment, and a position read earlier would not cover them. Once the
// configuration is in force, all the following commits wait, and the
// position read afterwards covers everything before them.
//
// Step 3 exists because backends don't read synchronous_standby_names: they
// look at a flag in shared memory that only the checkpointer updates, and it
// can take as long as a checkpoint to do it. ensureSyncStandbysEffective
// describes the details, including the limits of what we can guarantee.
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
		r.forgetSyncStandbysEffective()
		return false, nil
	}

	desired, err := r.getDesiredFailoverQuorumStatus(ctx)
	if err != nil {
		return false, err
	}

	if equality.Semantic.DeepEqual(desired, apiv1.FailoverQuorumStatus{}) {
		// Synchronous replication is not configured: when it will be, backends
		// may not wait for the standbys until the checkpointer notices.
		r.forgetSyncStandbysEffective()
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
			waiting, err := r.captureFailoverQuorum(ctx, cluster, desired, published)
			if err != nil || waiting {
				return waiting, err
			}
			published = apiv1.FailoverQuorumStatus{}
		}
	}
}

// captureFailoverQuorum empties the published status, which refers to a
// previous configuration and can't be trusted anymore, and records the
// WAL position the standbys have to reach before the desired status is
// published. It returns true when the caller should retry later.
func (r *InstanceReconciler) captureFailoverQuorum(
	ctx context.Context,
	cluster *apiv1.Cluster,
	desired apiv1.FailoverQuorumStatus,
	published apiv1.FailoverQuorumStatus,
) (bool, error) {
	contextLogger := log.FromContext(ctx)

	if !equality.Semantic.DeepEqual(published, apiv1.FailoverQuorumStatus{}) {
		if err := r.setFailoverQuorumStatus(ctx, cluster, apiv1.FailoverQuorumStatus{}); err != nil {
			return false, err
		}
	}

	// The LSN is taken only when the backends are known to wait
	// for the synchronous standbys.
	effective, err := r.ensureSyncStandbysEffective(ctx)
	if err != nil {
		return false, err
	}
	if !effective {
		return true, nil
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

	return false, nil
}

// ensureSyncStandbysEffective makes sure that, after a reload that defined the
// synchronous standbys, committing backends really wait for them. It returns
// false when this is not guaranteed yet and the caller should retry.
//
// # Why a checkpoint is needed
//
// Committing backends don't read synchronous_standby_names. SyncRepWaitForLSN
// returns immediately when the sync_standbys_status flag in shared memory has
// SYNC_STANDBY_INIT set and SYNC_STANDBY_DEFINED unset, whatever the setting
// says.
//
// Only the checkpointer updates that flag, in UpdateSharedMemoryConfig, which
// calls SyncRepUpdateSyncStandbysDefined. It does so when it starts, in
// ProcessCheckpointerInterrupts after each checkpoint, and in
// CheckpointWriteDelay, but there only if the checkpoint is neither fast nor
// behind schedule.
//
// A reload reaches the checkpointer through the postmaster:
// process_pm_reload_request loads the configuration in the postmaster and then
// forwards SIGHUP to its children with SignalChildren. The handler in the
// checkpointer only sets ConfigReloadPending.
//
// An idle checkpointer, or one running a paced checkpoint that is on
// schedule, applies the reload promptly. A checkpoint that is behind schedule
// or fast doesn't until it ends, which was measured at 51 seconds in the
// discussion below. During that time commits return without waiting for the
// standbys, even if they are listed.
//
// Walsenders are not affected: they reload on their own
// (WalSndHandleConfigReload and SyncRepInitConfig) and decide which standbys
// count with their own copy of the setting. The flag is the only part that
// depends on the checkpointer. See the "sync_standbys_defined and
// pg_stat_replication" thread on pgsql-hackers (October 2025):
// https://www.postgresql.org/message-id/CANwKhkMTkckNSoz0kUYFvaMDA_6g2uuABcvhoowu64e9e%2Bop%3DA%40mail.gmail.com
//
// # What we do
//
// There is no way to observe the flag, so we request a checkpoint. It is
// fast, and it completes only after a checkpoint that started after the
// request. The checkpointer applies a pending reload before starting a
// checkpoint, so when it completes the flag is in force.
//
// The flag stays set for the whole PostgreSQL run, until synchronous
// replication is disabled, so the checkpoint is requested only when we don't
// know that it is. A restarted postmaster starts from the configuration on
// disk, and the checkpointer applies it when it starts, with no reload
// pending.
//
// The checkpoint can take long, and requesting it again while the requested
// one is running makes PostgreSQL run yet another checkpoint. So it is
// requested once: if the request times out, the next loops check if a
// checkpoint started after it was requested has completed, looking at the
// redo location of the last checkpoint.
//
// # Limits
//
//   - A reload can reach the checkpointer after our checkpoint has been
//     requested, because the postmaster forwards it asynchronously. We wait
//     for the new configuration to be loaded before requesting the checkpoint,
//     which makes this very unlikely, but it is not excluded.
//   - If the request fails with an error other than the timeout, we can't
//     know whether it was received. It is made again, and PostgreSQL may run
//     an extra checkpoint.
func (r *InstanceReconciler) ensureSyncStandbysEffective(ctx context.Context) (bool, error) {
	contextLogger := log.FromContext(ctx)

	if r.syncStandbysEffective {
		return true, nil
	}

	if r.checkpointRequestedAtLSN != "" {
		started, err := r.instance.HasCheckpointStartedAtOrAfter(ctx, r.checkpointRequestedAtLSN)
		if err != nil {
			return false, err
		}
		if !started {
			return false, nil
		}

		r.syncStandbysEffective = true
		r.checkpointRequestedAtLSN = ""
		return true, nil
	}

	lsn, err := r.instance.GetCurrentWALInsertLSN(ctx)
	if err != nil {
		return false, err
	}
	r.checkpointRequestedAtLSN = lsn

	contextLogger.Info("Requesting a checkpoint to ensure the synchronous standbys are effective", "lsn", lsn)

	checkpointCtx, cancel := context.WithTimeout(ctx, failoverQuorumCheckpointTimeout)
	defer cancel()

	if err := r.instance.RequestCheckpoint(checkpointCtx); err != nil {
		if errors.Is(checkpointCtx.Err(), context.DeadlineExceeded) {
			contextLogger.Info("The checkpoint is taking long, will wait for it",
				"timeout", failoverQuorumCheckpointTimeout)
			return false, nil
		}

		// We don't know if the request has been received
		r.checkpointRequestedAtLSN = ""
		return false, err
	}

	r.syncStandbysEffective = true
	r.checkpointRequestedAtLSN = ""
	return true, nil
}

// forgetSyncStandbysEffective forgets that the synchronous standbys are
// effective, and any checkpoint requested to make them so
func (r *InstanceReconciler) forgetSyncStandbysEffective() {
	r.syncStandbysEffective = false
	r.checkpointRequestedAtLSN = ""
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
	// configuration backing `desired` became effective (see
	// ensureSyncStandbysEffective). Every transaction
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
