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
	"maps"
	"slices"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// evaluateReplicaDivergence records in cluster.Status.DivergedInstances the
// non-primary instances that report a timeline divergence. Each instance
// detects it from its own pg_wal (see postgres.DetectTimelineDivergence), so
// the operator only mirrors what the instances report:
//   - a reporting instance with a divergence gets an entry, created the
//     first time with an event and left as it is afterwards;
//   - a reporting instance without one, or running as primary, has its
//     entry removed;
//   - an instance that is not reporting, or is fenced (and so does not run
//     PostgreSQL), keeps its entry untouched, since there is nothing new to
//     say about it.
//
// Containment (fencing the instance and excluding it from the primary's
// replication slots) is handled separately by
// reconcileDivergedReplicaContainment.
//
// Replica-cluster topologies are out of scope: every instance there is a
// standby of an external primary.
func (r *ClusterReconciler) evaluateReplicaDivergence(
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
) {
	if cluster.IsReplica() {
		return
	}

	for i := range statuses.Items {
		item := &statuses.Items[i]
		if item.Pod == nil || item.Error != nil || item.IsFenced {
			continue
		}

		// A primary follows no other timeline: an entry for it is left over
		// from before it was promoted.
		name := apiv1.PodName(item.Pod.Name)
		if item.IsPrimary || item.Divergence == nil {
			delete(cluster.Status.DivergedInstances, name)
			continue
		}

		// The entry records the first report and is never refreshed: a
		// replica that is not fenced may keep replaying archived WAL of its
		// own timeline, and following it would rewrite the status on every
		// pass.
		if _, known := cluster.Status.DivergedInstances[name]; known {
			continue
		}

		if cluster.Status.DivergedInstances == nil {
			cluster.Status.DivergedInstances = make(map[apiv1.PodName]apiv1.DivergedInstanceStatus)
		}
		cluster.Status.DivergedInstances[name] = apiv1.DivergedInstanceStatus{
			TimeLineID:        item.Divergence.TimeLineID,
			PrimaryTimeLineID: item.Divergence.PrimaryTimeLineID,
			ForkLSN:           string(item.Divergence.ForkLSN),
			ReplayLSN:         string(item.Divergence.ReplayLSN),
			DetectedAt:        pgTime.GetCurrentTimestamp(),
		}
		r.Recorder.Eventf(cluster, "Warning", "ReplicaDiverged",
			"Instance %v replayed WAL up to %v on timeline %v, past the point (%v) where timeline %v "+
				"forked away from it: it can never follow the current primary and holds writes "+
				"that were discarded when the primary was promoted",
			name, item.Divergence.ReplayLSN, item.Divergence.TimeLineID,
			item.Divergence.ForkLSN, item.Divergence.PrimaryTimeLineID)
	}

	updateReplicasHealthyCondition(cluster)
}

// updateReplicasHealthyCondition reflects the current content of
// cluster.Status.DivergedInstances in the ConditionReplicasHealthy condition.
func updateReplicasHealthyCondition(cluster *apiv1.Cluster) {
	condition := metav1.Condition{
		Type:    string(apiv1.ConditionReplicasHealthy),
		Status:  metav1.ConditionTrue,
		Reason:  string(apiv1.ConditionReasonAllReplicasHealthy),
		Message: "No replica has diverged from the current primary's timeline.",
	}
	if len(cluster.Status.DivergedInstances) > 0 {
		names := make([]string, 0, len(cluster.Status.DivergedInstances))
		for name := range cluster.Status.DivergedInstances {
			names = append(names, string(name))
		}
		slices.Sort(names)
		condition.Status = metav1.ConditionFalse
		condition.Reason = string(apiv1.ConditionReasonReplicasDiverged)
		condition.Message = "Instances diverged from the current primary's timeline: " + strings.Join(names, ", ")
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

// reconcileDivergedReplicaContainment fences every instance in
// cluster.Status.DivergedInstances that isn't already fenced, and lifts
// containment for one whose PGDATA PVC has since been replaced by a fresh
// clone (e.g. via `kubectl cnpg destroy`).
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
	for _, name := range slices.Collect(maps.Keys(cluster.Status.DivergedInstances)) {
		// Re-read after every mutation below: the fencing executor refreshes
		// cluster in place from the API server as a side effect.
		diverged, ok := cluster.Status.DivergedInstances[name]
		if !ok {
			continue
		}

		if diverged.Parked {
			if err := r.liftContainmentIfRebuilt(ctx, cluster, name, diverged, pvcs); err != nil {
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
	diverged apiv1.DivergedInstanceStatus,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	currentPVC := findPgDataPVC(pvcs, string(name))
	if currentPVC == nil || string(currentPVC.UID) == diverged.PVCUID {
		return nil
	}

	if err := utils.NewFencingMetadataExecutor(r.Client).RemoveFencing().ForInstance(string(name)).
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return fmt.Errorf("lifting containment for rebuilt instance %s: %w", name, err)
	}

	origCluster := cluster.DeepCopy()
	delete(cluster.Status.DivergedInstances, name)
	updateReplicasHealthyCondition(cluster)
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return err
	}

	log.FromContext(ctx).Info("Lifted containment for a diverged replica whose data has been rebuilt",
		"instance", name)
	return nil
}

// parkDivergedReplica fences name as containment for a divergence and
// records its PGDATA PVC's UID, unless name is a primary-role instance,
// fencing it would break synchronous quorum, or its PGDATA PVC could not be
// found this pass.
func (r *ClusterReconciler) parkDivergedReplica(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	statuses postgres.PostgresqlStatusList,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	if string(name) == cluster.Status.CurrentPrimary || string(name) == cluster.Status.TargetPrimary {
		// Should never happen: detection already excludes primary-role
		// instances. Kept as a defensive backstop against ever fencing the
		// primary.
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

	// A pass reading a cache that has the previous pass's fence but not yet
	// its parked entry only has the bookkeeping left to record.
	alreadyFenced := cluster.IsInstanceFenced(string(name))
	if err := utils.NewFencingMetadataExecutor(r.Client).AddFencing().ForInstance(string(name)).
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return fmt.Errorf("fencing diverged replica %s: %w", name, err)
	}

	// The PVC UID tells a Pod recreated over the same, still diverged data
	// apart from a rebuild from a fresh clone.
	if diverged, ok := cluster.Status.DivergedInstances[name]; ok && !diverged.Parked {
		origCluster := cluster.DeepCopy()
		diverged.Parked = true
		diverged.PVCUID = string(currentPVC.UID)
		cluster.Status.DivergedInstances[name] = diverged
		if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			return err
		}
	}
	if alreadyFenced {
		return nil
	}

	contextLogger.Warning("Fenced a replica that diverged onto a discarded timeline",
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
