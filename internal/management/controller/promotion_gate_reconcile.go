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
	"fmt"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	// promotionGateEnforced is whether the promotion gate holds a promotion
	// back, the value that a new instance reconciler takes. When it is false
	// the gate only computes its decision and logs it, exactly as if it were
	// enforcing it, and never holds a promotion back.
	//
	// Enforcing it only ever holds back the promotion after a clean shutdown of
	// the previous primary, as a switchover: that is the only position that is
	// final. A failover, or a position that is not known, is reported and the
	// replica promotes.
	promotionGateEnforced = true

	// promotionBlockedRetryPeriod is how often a replica that the gate blocked
	// looks again, as a person may have unblocked it
	promotionBlockedRetryPeriod = 30 * time.Second

	// replicaPositionTimeout bounds the reading of the position of the replica
	replicaPositionTimeout = 30 * time.Second
)

// replicaPositionQuery returns the timeline and the position in the WAL of a
// replica. The WAL received but not replayed counts, as the promotion replays
// it first: the received position is NULL if the replica did not stream since
// it started, and it stays behind when the WAL comes from the archive, hence
// the GREATEST.
//
// There is no function of the current timeline of a replica, and the status of
// the WAL receiver is empty once the primary is gone: the one of the control
// file is what is left. It lags until the restartpoint that follows the
// checkpoint of the primary is done, hence the CHECKPOINT before reading it.
const replicaPositionQuery = `SELECT
	(SELECT timeline_id FROM pg_catalog.pg_control_checkpoint()),
	GREATEST(
		COALESCE(pg_catalog.pg_last_wal_receive_lsn(), '0/0'),
		COALESCE(pg_catalog.pg_last_wal_replay_lsn(), '0/0'))::text`

// queryReplicaPosition reads the position of this instance, which must be a replica
func (r *InstanceReconciler) queryReplicaPosition(ctx context.Context) (replicaPosition, error) {
	ctx, cancel := context.WithTimeout(ctx, replicaPositionTimeout)
	defer cancel()

	db, err := r.instance.GetSuperUserDB()
	if err != nil {
		return replicaPosition{}, fmt.Errorf("could not get the database connection pool: %w", err)
	}

	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return replicaPosition{}, fmt.Errorf("could not request a checkpoint: %w", err)
	}

	var result replicaPosition
	var lsn string
	if err := db.QueryRowContext(ctx, replicaPositionQuery).Scan(&result.Timeline, &lsn); err != nil {
		return replicaPosition{}, err
	}
	result.LSN = types.LSN(lsn)

	return result, nil
}

// promoteAfterGate promotes this instance, once the promotion gate allows it.
// The caller must have acquired the primary lease: the gate reads what the
// previous primary published on it, and nobody else writes it from then on.
//
// A result that is not empty means that the promotion has to wait, and the
// reconciliation has to be done again after that time.
func (r *InstanceReconciler) promoteAfterGate(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)

	// The promotion of a replica cluster is validated by its promotion token,
	// and the position on the lease is the one of an earlier life of this
	// cluster, which the gate would take for a diverged replica
	gateApplies := !cluster.ShouldPromoteFromReplicaCluster()

	readReplicaPosition := r.readReplicaPosition
	if readReplicaPosition == nil {
		readReplicaPosition = r.queryReplicaPosition
	}
	replica, err := readReplicaPosition(ctx)
	if err != nil {
		// What the gate does without it is its decision: it waits after a
		// clean shutdown of the previous primary, as it cannot check that the
		// replica has all the data, and it does not hold back anything else
		contextLogger.Warning("Cannot read the position of this replica, it is not available to the promotion gate",
			"err", err)
		replica = replicaPosition{}
	}

	if gateApplies {
		if result, err := r.checkPromotionGate(ctx, cluster, replica); err != nil || !result.IsZero() {
			return result, err
		}
	}

	// Consume the position of the previous primary, before it becomes
	// something that a later reader can take for the current one
	if err := r.primaryLeaseAcquirer.MarkPromoting(ctx, string(replica.LSN), replica.Timeline); err != nil {
		return reconcile.Result{}, fmt.Errorf("while marking the promotion on the primary lease: %w", err)
	}

	cluster.LogTimestampsWithMessage(ctx, "Setting myself as primary")
	return reconcile.Result{}, r.handlePromotion(ctx, cluster)
}

// checkPromotionGate reads what the previous primary published on the primary
// lease and decides, and logs, whether this replica can promote. It returns a
// result that is not empty when the promotion has to wait.
func (r *InstanceReconciler) checkPromotionGate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	replica replicaPosition,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)

	annotation, err := r.primaryLeaseAcquirer.Position(ctx)
	if err != nil {
		if r.enforcePromotionGate {
			return reconcile.Result{}, fmt.Errorf("while reading the position of the previous primary: %w", err)
		}
		contextLogger.Warning("Cannot read the position of the previous primary, skipping the promotion gate",
			"err", err)
		return reconcile.Result{}, nil
	}

	decision := decidePromotion(promotionGateInput{
		Annotation: annotation,
		PodName:    r.instance.GetPodName(),
		Replica:    replica,
	})
	logPromotionDecision(ctx, decision, r.enforcePromotionGate)

	if !r.enforcePromotionGate {
		return reconcile.Result{}, nil
	}

	switch decision.Verdict {
	case promotionWait:
		return reconcile.Result{RequeueAfter: cluster.GetPrimaryLeaseRetryPeriod()}, nil
	case promotionBlocked:
		return reconcile.Result{RequeueAfter: promotionBlockedRetryPeriod}, nil
	}

	return reconcile.Result{}, nil
}

// logMessage is what to log about a decision. A decision that holds the
// promotion back says so, and when the gate is not enforced it also says that
// the promotion goes on, so that the log does not claim what did not happen.
func (d promotionDecision) logMessage(enforced bool) string {
	if enforced || d.Verdict == promotionProceed {
		return d.Message
	}

	return d.Message + " (the promotion gate is not enforced, the promotion goes on)"
}

// logPromotionDecision logs a decision of the promotion gate at the level it
// asks for
func logPromotionDecision(ctx context.Context, decision promotionDecision, enforced bool) {
	contextLogger := log.FromContext(ctx)

	message := decision.logMessage(enforced)
	details := slices.Concat(decision.Details, []any{"verdict", decision.Verdict})
	switch decision.Level {
	case promotionLogError:
		contextLogger.Error(nil, message, details...)
	case promotionLogWarning:
		contextLogger.Warning(message, details...)
	default:
		contextLogger.Info(message, details...)
	}
}
