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
	"sort"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// replicaDivergenceStallGracePeriod is how long a replica's received LSN
// must be observed frozen, while it is behind the current primary's
// timeline, before the fork-point check that confirms a divergence runs.
//
// Comparable in spirit to walReceiversStalledGracePeriod (60s, see
// replicas.go): comfortably above status-collection and reconcile jitter, so
// a replica isn't flagged on a single stale poll, but short enough that a
// genuinely diverged replica doesn't retry a broken WAL stream unsurfaced
// for long. The clock for a given (instance, primary timeline) pair only
// starts once the current primary is reported Ready (see
// evaluateReplicaDivergence), so completing a failover never flags every
// survivor still draining the previous primary's backlog.
const replicaDivergenceStallGracePeriod = 60 * time.Second

// evaluateReplicaDivergence detects non-primary instances whose received WAL
// has stalled behind the current primary's timeline, confirms -- via a
// fork-point check against the current primary's timeline history -- whether
// they have progressed past the point where that timeline forked away from
// theirs, and records the result in cluster.Status.ReplicaWalIssues and
// cluster.Status.ReplicaDivergenceWatermarks.
//
// Detection and surfacing (this function) always run. Containment (fencing
// the instance and excluding it from the primary's replication slots) is
// handled separately by reconcileDivergedReplicaContainment and is gated by
// the `alpha.cnpg.io/divergedReplicaHandling` annotation.
//
// Replica-cluster topologies are out of scope (I1): every instance there is
// a standby of an external primary, so there is no local primary timeline to
// have forked from.
func (r *ClusterReconciler) evaluateReplicaDivergence(
	ctx context.Context,
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
) {
	if cluster.IsReplica() {
		return
	}

	var primary *postgres.PostgresqlStatus
	for i := range statuses.Items {
		if statuses.Items[i].IsPrimary && statuses.Items[i].IsPodReady {
			primary = &statuses.Items[i]
			break
		}
	}

	existingWatermarks := cluster.Status.ReplicaDivergenceWatermarks
	newWatermarks := make(map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark)

	for i := range statuses.Items {
		item := &statuses.Items[i]
		if item.Pod == nil || item.IsPrimary {
			// A5: never evaluate primary-role instances.
			continue
		}

		r.evaluateInstanceDivergence(ctx, cluster, item, primary, existingWatermarks, newWatermarks)
	}

	cluster.Status.ReplicaDivergenceWatermarks = newWatermarks
	updateReplicasHealthyCondition(cluster)
}

// evaluateInstanceDivergence applies the per-instance detection logic
// described in evaluateReplicaDivergence to a single non-primary instance,
// updating cluster.Status.ReplicaWalIssues and newWatermarks in place.
func (r *ClusterReconciler) evaluateInstanceDivergence(
	ctx context.Context,
	cluster *apiv1.Cluster,
	item *postgres.PostgresqlStatus,
	primary *postgres.PostgresqlStatus,
	existingWatermarks map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark,
	newWatermarks map[apiv1.PodName]apiv1.ReplicaDivergenceWatermark,
) {
	contextLogger := log.FromContext(ctx)
	name := apiv1.PodName(item.Pod.Name)

	if item.Error != nil {
		// Not reporting right now: preserve whatever watermark we had
		// untouched, and don't touch a latched issue -- nothing new to say
		// about this instance this pass.
		if prior, ok := existingWatermarks[name]; ok {
			newWatermarks[name] = prior
		}
		return
	}

	if item.TimeLineID >= cluster.Status.TimelineID && item.TimeLineID != 0 {
		// Caught up: drop any latched issue. This is also how a rebuilt
		// instance (e.g. via `kubectl cnpg destroy`) self-heals once it
		// streams far enough to report a fresh, current timeline.
		delete(cluster.Status.ReplicaWalIssues, name)
		return
	}

	if item.TimeLineID == 0 {
		// Unknown: fenced/masked (e.g. already parked) or still
		// bootstrapping. Preserve any existing watermark untouched; nothing
		// new to detect either way.
		if prior, ok := existingWatermarks[name]; ok {
			newWatermarks[name] = prior
		}
		return
	}

	if issue, ok := cluster.Status.ReplicaWalIssues[name]; ok && issue.AgainstTimeLineID == cluster.Status.TimelineID {
		// Already evaluated against the current primary timeline; wait for
		// containment (Diverged) or a fresh failover / catch-up (Stuck)
		// before re-running the fork-point check.
		return
	}

	if primary == nil {
		// A1: don't start the clock before the current primary is Ready, or
		// this fires the instant a failover completes and flags every
		// survivor still draining the previous primary's backlog.
		return
	}

	prior, hadPrior := existingWatermarks[name]
	if !hadPrior ||
		prior.TimeLineID != cluster.Status.TimelineID ||
		cnpgTypes.LSN(prior.ReceivedLSN).Less(item.ReceivedLsn) {
		// First observation for this (instance, timeline), or the received
		// LSN progressed: (re)start the clock.
		newWatermarks[name] = apiv1.ReplicaDivergenceWatermark{
			TimeLineID:  cluster.Status.TimelineID,
			ReceivedLSN: string(item.ReceivedLsn),
			Since:       pgTime.GetCurrentTimestamp(),
		}
		return
	}

	// Frozen at the same (timeline, LSN) we already knew about: carry the
	// watermark forward and check whether it has aged past the grace
	// period.
	newWatermarks[name] = prior

	stalledFor, err := pgTime.DifferenceBetweenTimestamps(pgTime.GetCurrentTimestamp(), prior.Since)
	if err != nil {
		contextLogger.Error(err, "while evaluating a replica divergence watermark's age", "instance", name)
		return
	}
	if stalledFor < replicaDivergenceStallGracePeriod {
		return
	}

	r.confirmReplicaDivergence(ctx, cluster, item, primary)
}

// confirmReplicaDivergence runs the fork-point check for a replica whose
// received WAL has stalled behind the current primary's timeline for at
// least replicaDivergenceStallGracePeriod, and records the outcome in
// cluster.Status.ReplicaWalIssues.
func (r *ClusterReconciler) confirmReplicaDivergence(
	ctx context.Context,
	cluster *apiv1.Cluster,
	replica *postgres.PostgresqlStatus,
	primary *postgres.PostgresqlStatus,
) {
	contextLogger := log.FromContext(ctx)
	name := apiv1.PodName(replica.Pod.Name)

	history, err := r.InstanceClient.GetTimelineHistoryFromInstance(ctx, primary.Pod)
	if err != nil {
		contextLogger.Error(err, "while fetching the current primary's timeline history to confirm "+
			"a possible replica divergence, will retry",
			"instance", name, "primary", primary.Pod.Name)
		return
	}

	forkLSN, found := postgres.FindTimelineForkPoint(history.Content, replica.TimeLineID)

	// B6: divergence requires the replica's received LSN to be strictly past
	// the fork point. A replica that never received WAL past the fork point
	// is stuck for some other reason (or the fork point is unknown), but has
	// not necessarily discarded any committed WAL.
	if !found || !forkLSN.Less(replica.ReceivedLsn) {
		r.recordReplicaWalIssue(cluster, name, apiv1.ReplicaWalIssueStuck, replica, forkLSN, found)
		return
	}

	r.recordReplicaWalIssue(cluster, name, apiv1.ReplicaWalIssueDiverged, replica, forkLSN, found)
	r.Recorder.Eventf(cluster, "Warning", "ReplicaDiverged",
		"Instance %v received WAL up to %v, past the point (%v) where the current primary's "+
			"timeline %v forked away from timeline %v: it can never catch up and holds writes "+
			"that were discarded when the primary was promoted",
		name, replica.ReceivedLsn, forkLSN, cluster.Status.TimelineID, replica.TimeLineID)
}

// recordReplicaWalIssue latches the outcome of a fork-point check for name
// into cluster.Status.ReplicaWalIssues.
func (r *ClusterReconciler) recordReplicaWalIssue(
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	kind apiv1.ReplicaWalIssueKind,
	replica *postgres.PostgresqlStatus,
	forkLSN cnpgTypes.LSN,
	forkLSNFound bool,
) {
	if cluster.Status.ReplicaWalIssues == nil {
		cluster.Status.ReplicaWalIssues = make(map[apiv1.PodName]apiv1.ReplicaWalIssueStatus)
	}

	status := apiv1.ReplicaWalIssueStatus{
		Kind:               kind,
		DetectedTimeLineID: replica.TimeLineID,
		AgainstTimeLineID:  cluster.Status.TimelineID,
		ReceivedLSN:        string(replica.ReceivedLsn),
		DetectedAt:         pgTime.GetCurrentTimestamp(),
	}
	if forkLSNFound {
		status.ForkLSN = string(forkLSN)
	}

	cluster.Status.ReplicaWalIssues[name] = status
}

// updateReplicasHealthyCondition reflects the current content of
// cluster.Status.ReplicaWalIssues in the ConditionReplicasHealthy condition.
func updateReplicasHealthyCondition(cluster *apiv1.Cluster) {
	if len(cluster.Status.ReplicaWalIssues) == 0 {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    string(apiv1.ConditionReplicasHealthy),
			Status:  metav1.ConditionTrue,
			Reason:  string(apiv1.ConditionReasonAllReplicasHealthy),
			Message: "No replica is stalled behind the current primary's timeline.",
		})
		return
	}

	names := make([]string, 0, len(cluster.Status.ReplicaWalIssues))
	for name := range cluster.Status.ReplicaWalIssues {
		names = append(names, string(name))
	}
	sort.Strings(names)

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    string(apiv1.ConditionReplicasHealthy),
		Status:  metav1.ConditionFalse,
		Reason:  string(apiv1.ConditionReasonReplicaWalIssues),
		Message: fmt.Sprintf("Instances stalled behind the current primary's timeline: %s", strings.Join(names, ", ")),
	})
}

// reconcileDivergedReplicaContainment fences every instance confirmed
// diverged in cluster.Status.ReplicaWalIssues that isn't already fenced, and
// lifts containment for one whose PGDATA PVC has since been replaced by a
// fresh clone (e.g. via `kubectl cnpg destroy`). Gated by the
// `alpha.cnpg.io/divergedReplicaHandling` annotation: when set to
// `detectOnly`, the divergence stays surfaced but nothing is fenced.
//
// Called once no switchover or failover is in progress and no primary
// transition happened this reconcile pass (see the call site in
// cluster_controller.go), so containment is naturally deferred while a
// primary election is underway.
func (r *ClusterReconciler) reconcileDivergedReplicaContainment(
	ctx context.Context,
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	names := make([]apiv1.PodName, 0, len(cluster.Status.ReplicaWalIssues))
	for name, issue := range cluster.Status.ReplicaWalIssues {
		if issue.Kind == apiv1.ReplicaWalIssueDiverged {
			names = append(names, name)
		}
	}

	for _, name := range names {
		// Re-read after every mutation below: setInstanceFencing refreshes
		// cluster in place from the API server as a side effect.
		issue, ok := cluster.Status.ReplicaWalIssues[name]
		if !ok || issue.Kind != apiv1.ReplicaWalIssueDiverged {
			continue
		}

		if issue.Parked {
			if err := r.liftContainmentIfRebuilt(ctx, cluster, name, issue, pvcs); err != nil {
				return err
			}
			continue
		}

		if err := r.parkDivergedReplica(ctx, cluster, name, statuses, pvcs); err != nil {
			return err
		}
	}

	return nil
}

// liftContainmentIfRebuilt lifts fencing and forgets a parked instance's
// entry once its PGDATA PVC has been replaced by a fresh clone (e.g. via
// `kubectl cnpg destroy`, which deletes and recreates the PVC). A Pod
// recreation alone (node failure, eviction, rollout) reuses the same PVC and
// must NOT lift containment, since the data is still diverged: only a PVC
// UID change proves the data was actually rebuilt.
func (r *ClusterReconciler) liftContainmentIfRebuilt(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	issue apiv1.ReplicaWalIssueStatus,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	currentPVC := findPgDataPVC(pvcs, string(name))
	if currentPVC == nil || issue.PVCUID == "" || string(currentPVC.UID) == issue.PVCUID {
		return nil
	}

	if err := r.setInstanceFencing(ctx, cluster, string(name), false); err != nil {
		return fmt.Errorf("lifting containment for rebuilt instance %s: %w", name, err)
	}
	if err := r.clearReplicaWalIssue(ctx, cluster, name); err != nil {
		return err
	}

	log.FromContext(ctx).Info("Lifted containment for a diverged replica whose data has been rebuilt",
		"instance", name)
	return nil
}

// parkDivergedReplica fences name as containment for a confirmed divergence
// and records its PGDATA PVC's UID, unless the kill-switch annotation
// disables containment, name is a primary-role instance, fencing it would
// break synchronous quorum, or its PGDATA PVC could not be found this pass.
func (r *ClusterReconciler) parkDivergedReplica(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	statuses postgres.PostgresqlStatusList,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	if cluster.GetDivergedReplicaHandlingMode() == apiv1.DivergedReplicaHandlingDetectOnly {
		return nil
	}

	if string(name) == cluster.Status.CurrentPrimary || string(name) == cluster.Status.TargetPrimary {
		// Should never happen: A5 already excludes primary-role instances at
		// detection time. Kept as a defensive backstop against ever fencing
		// the primary.
		contextLogger.Warning("Refusing to fence a diverged instance in a primary role", "instance", name)
		return nil
	}

	if divergedReplicaParkWouldBreakSyncQuorum(cluster, statuses, string(name)) {
		contextLogger.Warning(
			"Not fencing a diverged replica because it would drop the cluster below the "+
				"required synchronous replication quorum; the divergence remains surfaced",
			"instance", name)
		return nil
	}

	currentPVC := findPgDataPVC(pvcs, string(name))
	if currentPVC == nil {
		contextLogger.Warning(
			"Deferring containment of a diverged replica: its PGDATA PVC was not found this pass",
			"instance", name)
		return nil
	}

	if err := r.setInstanceFencing(ctx, cluster, string(name), true); err != nil {
		return fmt.Errorf("fencing diverged replica %s: %w", name, err)
	}

	if err := r.markReplicaWalIssueParked(ctx, cluster, name, string(currentPVC.UID)); err != nil {
		return err
	}

	contextLogger.Warning("Fenced a replica confirmed to have diverged onto a discarded timeline",
		"instance", name)
	r.Recorder.Eventf(cluster, "Warning", "ReplicaDiverged",
		"Fenced instance %v: rebuild it with `kubectl cnpg destroy %v %v`",
		name, cluster.Name, name)
	return nil
}

// findPgDataPVC returns the PGDATA PersistentVolumeClaim for the named
// instance, if present in pvcs. An instance's other PVCs (WAL storage,
// tablespaces) use a different name and are never returned.
func findPgDataPVC(pvcs []corev1.PersistentVolumeClaim, instanceName string) *corev1.PersistentVolumeClaim {
	for i := range pvcs {
		if pvcs[i].Name == instanceName && pvcs[i].Labels[utils.PvcRoleLabelName] == string(utils.PVCRolePgData) {
			return &pvcs[i]
		}
	}
	return nil
}

// divergedReplicaParkWouldBreakSyncQuorum reports whether fencing the named
// instance would drop the number of available replicas below the
// synchronous replication requirement, in which case containment must fall
// back to surface-only.
func divergedReplicaParkWouldBreakSyncQuorum(
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
	excluding string,
) bool {
	sync := cluster.Spec.PostgresConfiguration.Synchronous
	if sync == nil || sync.Number <= 0 {
		return false
	}

	var available int
	for i := range statuses.Items {
		item := &statuses.Items[i]
		if item.Pod == nil || item.IsPrimary || item.Pod.Name == excluding {
			continue
		}
		if item.Error == nil && item.IsPodReady {
			available++
		}
	}

	return available < sync.Number
}

// setInstanceFencing adds or removes instanceName from the cluster's fenced
// instances. Note this refreshes cluster in place from a fresh Get against
// the API server as a side effect (see utils.FencingMetadataExecutor).
func (r *ClusterReconciler) setInstanceFencing(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instanceName string,
	fence bool,
) error {
	executor := utils.NewFencingMetadataExecutor(r.Client)
	if fence {
		executor = executor.AddFencing()
	} else {
		executor = executor.RemoveFencing()
	}

	return executor.ForInstance(instanceName).Execute(ctx, client.ObjectKeyFromObject(cluster), cluster)
}

// markReplicaWalIssueParked records that name has been fenced as containment
// for a confirmed divergence, together with the UID of its PGDATA PVC at
// that moment -- the reference point later used to tell a mere Pod
// recreation (same PVC, still diverged) apart from a real rebuild (a fresh
// PVC from a re-clone, safe to lift containment for).
func (r *ClusterReconciler) markReplicaWalIssueParked(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	pvcUID string,
) error {
	issue, ok := cluster.Status.ReplicaWalIssues[name]
	if !ok || issue.Parked {
		return nil
	}

	origCluster := cluster.DeepCopy()
	issue.Parked = true
	issue.PVCUID = pvcUID
	cluster.Status.ReplicaWalIssues[name] = issue
	return r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster))
}

// clearReplicaWalIssue forgets any latched issue and divergence watermark
// for name.
func (r *ClusterReconciler) clearReplicaWalIssue(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
) error {
	_, hasIssue := cluster.Status.ReplicaWalIssues[name]
	_, hasWatermark := cluster.Status.ReplicaDivergenceWatermarks[name]
	if !hasIssue && !hasWatermark {
		return nil
	}

	origCluster := cluster.DeepCopy()
	delete(cluster.Status.ReplicaWalIssues, name)
	delete(cluster.Status.ReplicaDivergenceWatermarks, name)
	updateReplicasHealthyCondition(cluster)
	return r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster))
}
