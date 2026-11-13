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

package lease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// liveQueryTimeout bounds the query of the live position. It sits on the path
// of the renewals of the lease, where a hung PostgreSQL must not eat the budget
// that monitorLeaseAge and RenewDeadline rely on.
const liveQueryTimeout = 1 * time.Second

// livePositionQuery returns the flush LSN of a primary and the name of the WAL
// file it falls in, which starts with the current timeline. Nothing is returned
// in recovery: the annotation belongs to the previous primary until this
// instance is one.
//
// The timeline is taken from the WAL file name, not from the control file: the
// latter keeps the old timeline until the checkpoint that follows a promotion
// completes, while the insertion timeline switches as soon as recovery ends.
const livePositionQuery = `SELECT pg_catalog.pg_current_wal_flush_lsn()::text,
	pg_catalog.pg_walfile_name(pg_catalog.pg_current_wal_flush_lsn())
 WHERE NOT pg_catalog.pg_is_in_recovery()`

// leaseAPITimeout bounds the calls to the API server that read the position of
// the previous primary and mark the promotion
const leaseAPITimeout = 5 * time.Second

// positionSource is what the client of the lock calls before writing the lease
// to know the position to publish: nil leaves the annotation as it is.
//
// The holder of the record being written decides the source, an empty holder
// meaning Release.
func (r *Runnable) positionSource(ctx context.Context, lease *coordinationv1.Lease) *utils.LeasePosition {
	if holder := lease.Spec.HolderIdentity; holder != nil && *holder != "" {
		return r.livePosition(ctx)
	}

	return r.releasePosition(ctx)
}

// livePosition returns the position of this instance while it is running as a
// primary. It returns nil, and the lease is written all the same, when this
// instance is not a primary or when the position cannot be read in time.
func (r *Runnable) livePosition(ctx context.Context) *utils.LeasePosition {
	contextLogger := log.FromContext(ctx)

	ctx, cancel := context.WithTimeout(ctx, liveQueryTimeout)
	defer cancel()

	lsn, walFile, err := r.queryLivePosition(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			contextLogger.Debug("Cannot read the position of the primary, leaving it as it is on the lease", "err", err)
		}
		return nil
	}

	// The WAL file name starts with the timeline, 8 hexadecimal digits
	if len(walFile) < 8 {
		contextLogger.Debug("Unexpected WAL file name, leaving the position as it is on the lease", "walFile", walFile)
		return nil
	}
	timeline, err := strconv.ParseUint(walFile[:8], 16, 32)
	if err != nil {
		contextLogger.Debug("Unexpected WAL file name, leaving the position as it is on the lease",
			"walFile", walFile, "err", err)
		return nil
	}

	return &utils.LeasePosition{
		LSN:      lsn,
		Timeline: int(timeline),
		State:    utils.PgDataStateInProduction,
		Pod:      r.instance.GetPodName(),
	}
}

// queryInstanceLivePosition runs the query of the live position against the
// local instance. It returns sql.ErrNoRows when the instance is in recovery.
func (r *Runnable) queryInstanceLivePosition(ctx context.Context) (string, string, error) {
	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return "", "", err
	}

	var lsn, walFile string
	if err := db.QueryRowContext(ctx, livePositionQuery).Scan(&lsn, &walFile); err != nil {
		return "", "", err
	}

	return lsn, walFile, nil
}

// releasePosition returns the final position of this instance, from the control
// file of PGDATA. PostgreSQL has exited by the time the lease is released, so
// the control file is what is left of it.
//
// It returns nil when the annotation has to stay as it is: this instance was
// never a primary since it took the lease, so the position of the previous
// holder is still the one that matters.
func (r *Runnable) releasePosition(ctx context.Context) *utils.LeasePosition {
	contextLogger := log.FromContext(ctx)
	podName := r.instance.GetPodName()

	unknown := func(err error) *utils.LeasePosition {
		contextLogger.Error(err, "Cannot determine the final position of this instance, "+
			"publishing it as unknown with the primary lease")
		return &utils.LeasePosition{State: utils.LeasePositionStateUnknown, Pod: podName}
	}

	output, err := r.readControldata()
	if err != nil {
		return unknown(err)
	}
	controlData := utils.ParsePgControldataOutput(output)

	state := utils.PgDataState(controlData.GetDatabaseClusterState())
	switch state {
	case utils.PgDataStateShutDown, utils.PgDataStateShuttingDown, utils.PgDataStateInProduction:
		// There is a position to publish, see below

	case utils.PgDataStateShutDownInRecovery,
		utils.PgDataStateInArchiveRecovery,
		utils.PgDataStateInCrashRecovery,
		utils.PgDataStateStartingUp:
		contextLogger.Warning("This instance was not a primary, leaving the position of the previous "+
			"primary on the primary lease", "state", state)
		return nil

	default:
		return unknown(fmt.Errorf("unexpected cluster state in pg_controldata: %q", state))
	}

	lsn := controlData.GetLatestCheckpointREDOLocation()
	if _, err := types.LSN(lsn).Parse(); err != nil {
		return unknown(fmt.Errorf("invalid latest checkpoint's REDO location in pg_controldata: %w", err))
	}

	timelineText, _ := controlData.TryGetLatestCheckpointTimelineID()
	timeline, err := strconv.Atoi(timelineText)
	if err != nil {
		return unknown(fmt.Errorf("invalid latest checkpoint's TimeLineID in pg_controldata: %w", err))
	}

	position := &utils.LeasePosition{
		LSN:      lsn,
		Timeline: timeline,
		State:    state,
		Pod:      podName,
	}

	if state == utils.PgDataStateShutDown {
		contextLogger.Info("Publishing the final position with the primary lease", "position", position)
	} else {
		contextLogger.Warning("PostgreSQL did not stop cleanly, the position published with the "+
			"primary lease is only a lower bound", "position", position)
	}

	return position
}

// getHeldLease reads the lease, and checks that this pod is the holder: the
// claim was a conditional write, so nobody but this pod writes the annotation
// while it holds the lease.
func (r *Runnable) getHeldLease(ctx context.Context) (*coordinationv1.Lease, error) {
	lease, err := r.leases.Get(ctx, r.instance.GetClusterName(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("while reading the primary lease: %w", err)
	}

	if holder := lease.Spec.HolderIdentity; holder == nil || *holder != r.lock.Identity() {
		return nil, fmt.Errorf("the primary lease is not held by this pod, the holder is %q", ptr.Deref(holder, ""))
	}

	return lease, nil
}

// Position returns the position that the previous primary published on the
// primary lease, as the value of the annotation. The value is empty if the
// lease has none. It fails when this pod does not hold the lease.
func (r *Runnable) Position(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, leaseAPITimeout)
	defer cancel()

	lease, err := r.getHeldLease(ctx)
	if err != nil {
		return "", err
	}

	return lease.Annotations[utils.LeasePositionAnnotationName], nil
}

// MarkPromoting replaces the position of the previous primary on the primary
// lease with the one of this replica, marked as promoting. It is done before
// the promotion, to consume the position of the previous primary: the position
// of a primary that is no longer current must not be read again.
//
// It fails when this pod does not hold the lease, and it retries on a conflict
// with the renewals of the elector.
func (r *Runnable) MarkPromoting(ctx context.Context, lsn string, timeline int) error {
	contextLogger := log.FromContext(ctx)

	position := utils.LeasePosition{
		LSN:      lsn,
		Timeline: timeline,
		State:    utils.LeasePositionStatePromoting,
		Pod:      r.instance.GetPodName(),
	}
	value, err := position.Marshal()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, leaseAPITimeout)
	defer cancel()

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		lease, err := r.getHeldLease(ctx)
		if err != nil {
			return err
		}

		if lease.Annotations == nil {
			lease.Annotations = make(map[string]string, 1)
		}
		lease.Annotations[utils.LeasePositionAnnotationName] = value

		_, err = r.leases.Update(ctx, lease, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}

	// The position is overwritten by the first write of the new primary: this
	// is the only trace of what this replica published before promoting
	contextLogger.Info("Marked the promotion on the primary lease", "position", position)
	return nil
}
