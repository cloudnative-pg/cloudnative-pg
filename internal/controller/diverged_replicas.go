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
//   - a reporting instance running as primary, or one that reports no
//     divergence while it is available and its entry is not parked, has its
//     entry removed;
//   - an instance that is not reporting, or is fenced (and so does not run
//     PostgreSQL), keeps its entry untouched, since there is nothing new to
//     say about it.
//
// Containment (fencing the instance and excluding it from the primary's
// replication slots) is handled separately by
// reconcileDivergedReplicaContainment.
//
// Replica-cluster topologies are not reported or contained: every instance
// there is a standby of an external primary. Their instances still fail the
// readiness probe when they diverge.
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
		if item.IsPrimary {
			delete(cluster.Status.DivergedInstances, name)
			continue
		}

		// No report of a divergence clears the entry, unless the instance
		// might be unavailable (it is starting after its fence was lifted, so
		// it cannot tell) or its data is parked: that data stays diverged
		// until its PVC is replaced.
		if item.Divergence == nil {
			if !item.MightBeUnavailable && !cluster.Status.DivergedInstances[name].Parked {
				delete(cluster.Status.DivergedInstances, name)
			}
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
		condition.Status = metav1.ConditionFalse
		condition.Reason = string(apiv1.ConditionReasonReplicasDiverged)
		condition.Message = "Instances diverged from the current primary's timeline: " +
			strings.Join(divergedInstanceNames(cluster), ", ")
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

// divergedInstanceNames returns the sorted names of the instances in
// cluster.Status.DivergedInstances.
func divergedInstanceNames(cluster *apiv1.Cluster) []string {
	names := make([]string, 0, len(cluster.Status.DivergedInstances))
	for name := range cluster.Status.DivergedInstances {
		names = append(names, string(name))
	}
	slices.Sort(names)
	return names
}

// updateNoPromotableInstanceCondition sets the ConditionNoPromotableInstance
// condition while the only active instances are diverged, and removes it
// otherwise. A diverged instance is fenced, and with
// PostgreSQL down it no longer reports its divergence:
// cluster.Status.DivergedInstances tells which ones they are.
func updateNoPromotableInstanceCondition(cluster *apiv1.Cluster, statuses postgres.PostgresqlStatusList) {
	// With no active instance there is nothing new to say
	if len(statuses.Items) == 0 {
		return
	}

	onlyDivergedInstances := !slices.ContainsFunc(statuses.Items, func(item postgres.PostgresqlStatus) bool {
		_, diverged := cluster.Status.DivergedInstances[apiv1.PodName(item.Pod.Name)]
		return !diverged
	})
	if !onlyDivergedInstances {
		meta.RemoveStatusCondition(&cluster.Status.Conditions, string(apiv1.ConditionNoPromotableInstance))
		return
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:   string(apiv1.ConditionNoPromotableInstance),
		Status: metav1.ConditionTrue,
		Reason: string(apiv1.ConditionReasonOnlyDivergedInstances),
		Message: fmt.Sprintf(
			"No instance can replace the primary %v: the only active instances are diverged, "+
				"and a diverged instance is never promoted: %v",
			cluster.Status.CurrentPrimary, strings.Join(divergedInstanceNames(cluster), ", ")),
	})
}

// reconcileDivergedReplicaContainment keeps every instance in
// cluster.Status.DivergedInstances fenced until its PGDATA PVC is replaced
// (for example via `kubectl cnpg destroy`) or removed.
//
// The entry is the source of truth and the fence is asserted on every pass,
// so a fence lifted by hand, or wiped by a fence on the whole cluster that
// was later lifted, is set again. The fence on the whole cluster is never
// added or removed here: while it is set it keeps PostgreSQL down.
//
// Called once no switchover or failover is in progress and no primary
// transition happened this reconcile pass (see the call site in
// cluster_controller.go), so containment is naturally deferred while a
// primary election is underway.
func (r *ClusterReconciler) reconcileDivergedReplicaContainment(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
	pods []corev1.Pod,
) error {
	for _, name := range slices.Collect(maps.Keys(cluster.Status.DivergedInstances)) {
		// Re-read after every mutation below: the fencing executor refreshes
		// cluster in place from the API server as a side effect.
		diverged, ok := cluster.Status.DivergedInstances[name]
		if !ok {
			continue
		}

		// The PVC list only holds the PVCs owned by the cluster: a PVC
		// detached by `kubectl cnpg destroy --keep-pvc` is missing from it
		// while its Pod is still running, so a missing PVC proves nothing
		// until the Pod is gone too.
		currentPVC := findPgDataPVC(pvcs, string(name))
		podExists := slices.ContainsFunc(pods, func(pod corev1.Pod) bool { return pod.Name == string(name) })
		rebuilt := currentPVC != nil && string(currentPVC.UID) != diverged.PVCUID
		removed := currentPVC == nil && !podExists
		if diverged.Parked && (rebuilt || removed) {
			if err := r.liftContainment(ctx, cluster, name); err != nil {
				return err
			}
			continue
		}

		if err := r.containDivergedReplica(ctx, cluster, name, currentPVC); err != nil {
			return err
		}
	}

	return nil
}

// liftContainment lifts the fencing and forgets the entry of a parked
// instance whose PGDATA PVC was replaced by a fresh clone (e.g. via
// `kubectl cnpg destroy`, which deletes and recreates the PVC) or removed.
// A Pod recreation alone (node failure, eviction, rollout) reuses the same
// PVC and must NOT lift containment, since the data is still diverged: only
// a PVC change proves the diverged data is gone.
func (r *ClusterReconciler) liftContainment(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
) error {
	err := utils.NewFencingMetadataExecutor(r.Client).RemoveFencing().ForInstance(string(name)).
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster)
	// A fence on the whole cluster is not ours to lift
	if err != nil && !errors.Is(err, utils.ErrorSingleInstanceUnfencing) {
		return fmt.Errorf("lifting containment for instance %s: %w", name, err)
	}

	origCluster := cluster.DeepCopy()
	delete(cluster.Status.DivergedInstances, name)
	updateReplicasHealthyCondition(cluster)
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return err
	}

	log.FromContext(ctx).Info("Lifted containment for a diverged replica whose data is gone", "instance", name)
	return nil
}

// containDivergedReplica fences name if it isn't fenced, and records the UID
// of its PGDATA PVC in its entry. It does nothing for a primary-role
// instance, or when the PGDATA PVC of an unparked entry could not be found
// this pass.
func (r *ClusterReconciler) containDivergedReplica(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name apiv1.PodName,
	currentPVC *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)
	parked := cluster.Status.DivergedInstances[name].Parked

	if string(name) == cluster.Status.CurrentPrimary || string(name) == cluster.Status.TargetPrimary {
		// Should never happen: detection already excludes primary-role
		// instances. Kept as a defensive backstop against ever fencing the
		// primary.
		contextLogger.Warning("Refusing to fence a diverged instance in a primary role", "instance", name)
		return nil
	}

	if currentPVC == nil && !parked {
		contextLogger.Warning(
			"Deferring containment of a diverged replica: its PGDATA PVC was not found this pass",
			"instance", name)
		return nil
	}

	// A pass reading a cache that has the previous pass's fence but not yet
	// its parked entry only has the bookkeeping left to record.
	wasFenced := cluster.IsInstanceFenced(string(name))
	if !wasFenced {
		if err := utils.NewFencingMetadataExecutor(r.Client).AddFencing().ForInstance(string(name)).
			Execute(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
			return fmt.Errorf("fencing diverged replica %s: %w", name, err)
		}
	}

	// The PVC UID tells a Pod recreated over the same, still diverged data
	// apart from a rebuild from a fresh clone.
	if diverged, ok := cluster.Status.DivergedInstances[name]; ok && !diverged.Parked && currentPVC != nil {
		origCluster := cluster.DeepCopy()
		diverged.Parked = true
		diverged.PVCUID = string(currentPVC.UID)
		cluster.Status.DivergedInstances[name] = diverged
		if err := r.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			return err
		}
	}
	if wasFenced {
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
