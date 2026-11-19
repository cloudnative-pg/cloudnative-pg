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
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	// promotionBlockedRetryPeriod is how often a replica that the gate blocked
	// looks again, as a person may have unblocked it
	promotionBlockedRetryPeriod = 30 * time.Second

	// promotionHeldBackReason is the reason of the event emitted when the
	// promotion gate holds a promotion back
	promotionHeldBackReason = "PromotionHeldBack"

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
// handlePromotion marks the promotion on the lease.
//
// A result that is not empty means that the promotion has to wait, and the
// reconciliation has to be done again after that time.
func (r *InstanceReconciler) promoteAfterGate(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx)

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

	if result, err := r.checkPromotionGate(ctx, cluster, replica); err != nil || !result.IsZero() {
		return result, err
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
	annotation, age, err := r.primaryLeaseAcquirer.Position(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("while reading the position of the previous primary: %w", err)
	}

	decision := decidePromotion(promotionGateInput{
		Annotation:    annotation,
		PodName:       r.instance.GetPodName(),
		Replica:       replica,
		AnnotationAge: age,
		// The promotion of a replica cluster is validated by its promotion
		// token, and the position on the lease is the one of an earlier life
		// of this cluster
		ReplicaClusterPromotion: cluster.ShouldPromoteFromReplicaCluster(),
	})
	logPromotionDecision(ctx, decision)

	switch decision.Verdict {
	case promotionWait:
		r.reportPromotionHeldBack(cluster, decision)
		return reconcile.Result{RequeueAfter: cluster.GetPrimaryLeaseRetryPeriod()}, nil
	case promotionBlocked:
		r.reportPromotionHeldBack(cluster, decision)
		return reconcile.Result{RequeueAfter: promotionBlockedRetryPeriod}, nil
	}

	return reconcile.Result{}, nil
}

// reportPromotionHeldBack emits an event on the Cluster saying why the
// promotion is held back. It is emitted at every evaluation, and relies on the
// event recorder to count the repetitions of an event that has the same
// message, which is why a decision keeps what changes between evaluations in
// its details and not in its message.
func (r *InstanceReconciler) reportPromotionHeldBack(cluster *apiv1.Cluster, decision promotionDecision) {
	if r.recorder == nil {
		return
	}

	// A promotion that waits is not told how to go on anyway by the decision,
	// the event does: it is where a person looks when the switchover does not complete
	message := decision.Message
	if !strings.Contains(message, unblockPromotionHint) {
		message += ". " + unblockPromotionHint
	}

	r.recorder.Event(cluster, corev1.EventTypeWarning, promotionHeldBackReason, message)
}

// logPromotionDecision logs a decision of the promotion gate at the level it
// asks for
func logPromotionDecision(ctx context.Context, decision promotionDecision) {
	contextLogger := log.FromContext(ctx)

	details := slices.Concat(decision.Details, []any{"verdict", decision.Verdict})
	switch decision.Level {
	case promotionLogError:
		contextLogger.Error(nil, decision.Message, details...)
	case promotionLogWarning:
		contextLogger.Warning(decision.Message, details...)
	default:
		contextLogger.Info(decision.Message, details...)
	}
}
