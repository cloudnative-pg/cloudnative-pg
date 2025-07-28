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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// evaluateQuorumCheck evaluate the quorum check algorithm to detect if a failover
// is possible without losing any transaction.
// "true" is returned when there is surely a replica containing all the transactions,
// "false" is returned otherwise.
// When an error is raised, the caller should not start a failover.
func (r *ClusterReconciler) evaluateQuorumCheck(
	ctx context.Context,
	cluster *apiv1.Cluster,
	statusList postgres.PostgresqlStatusList,
) (bool, error) {
	contextLogger := log.FromContext(ctx).WithValues("tag", "quorumCheck")

	var failoverQuorum apiv1.FailoverQuorum
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), &failoverQuorum); err != nil {
		if apierrs.IsNotFound(err) {
			contextLogger.Warning(
				"Quorum check failed because no synchronous metadata is available. Denying the failover request")
			return false, nil
		}

		contextLogger.Error(err,
			"Quorum check failed because the synchronous replica metadata couldn't be read")
		return false, err
	}

	return r.evaluateQuorumCheckWithStatus(ctx, &failoverQuorum, statusList)
}

// evaluateQuorumCheckWithStatus is used internally by evaluateQuorumCheck,
// primarily at the benefit of the unit tests
func (r *ClusterReconciler) evaluateQuorumCheckWithStatus(
	ctx context.Context,
	failoverQuorum *apiv1.FailoverQuorum,
	statusList postgres.PostgresqlStatusList,
) (bool, error) {
	contextLogger := log.FromContext(ctx).WithValues("tag", "quorumCheck")

	syncStatus := failoverQuorum.Status
	contextLogger.Trace("Dumping latest synchronous replication status", "syncStatus", syncStatus)

	// Step 1: coherence check of the synchrouous replication information
	if syncStatus.StandbyNumber <= 0 {
		contextLogger.Warning(
			"Quorum check failed a unsupported synchronous nodes number")
		return false, nil
	}

	if len(syncStatus.StandbyNames) == 0 {
		contextLogger.Warning(
			"Quorum check failed because the list of synchronous replicas is empty")
		return false, nil
	}

	// Step 2: detect promotable replicas
	candidateReplicas := stringset.New()
	for _, record := range statusList.Items {
		if record.Error == nil && record.IsPodReady {
			candidateReplicas.Put(record.Pod.Name)
		}
	}

	// Step 3: evaluate quorum check algorithm
	//
	// Important: R + W > N <==> strong consistency
	// With:
	// N = the cardinality of the synchronous_standby_names set
	// W = the sync number or 0 if we're changing a replica configuration.
	// R = the cardinality of the set of promotable replicas within the
	//     synchronous_standby_names set
	//
	// When this criteria is satisfied we surely have a node containing
	// the latest transaction.
	//
	// The case having W == 0 has been already sorted out in the coherence check.

	nodeSet := stringset.From(syncStatus.StandbyNames)
	writeSetCardinality := syncStatus.StandbyNumber
	readSet := nodeSet.Intersect(candidateReplicas)

	nodeSetCardinality := nodeSet.Len()
	readSetCardinality := readSet.Len()

	isStronglyConsistent := (readSetCardinality + writeSetCardinality) > nodeSetCardinality

	contextLogger.Info(
		"Quorum check algorithm results",
		"isStronglyConsistent", isStronglyConsistent,
		"readSetCardinality", readSetCardinality,
		"readSet", readSet.ToSortedList(),
		"writeSetCardinality", writeSetCardinality,
		"nodeSet", nodeSet.ToSortedList(),
		"nodeSetCardinality", nodeSetCardinality,
	)

	if !isStronglyConsistent {
		contextLogger.Info("Strong consistency check failed. Preventing failover.")
	}

	return isStronglyConsistent, nil
}

func (r *ClusterReconciler) reconcileFailoverQuorumObject(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithValues("tag", "quorumCheck")

	syncConfig := cluster.Spec.PostgresConfiguration.Synchronous
	failoverQuorumActive, err := cluster.IsFailoverQuorumActive()
	if err != nil {
		contextLogger.Error(err, "Failed to determine if failover quorum is active")
	}
	if syncConfig != nil && failoverQuorumActive {
		return r.ensureFailoverQuorumObjectExists(ctx, cluster)
	}

	return r.ensureFailoverQuorumObjectDoesNotExist(ctx, cluster)
}

func (r *ClusterReconciler) ensureFailoverQuorumObjectExists(ctx context.Context, cluster *apiv1.Cluster) error {
	failoverQuorum := apiv1.FailoverQuorum{
		TypeMeta: metav1.TypeMeta{
			Kind:       "FailoverQuorum",
			APIVersion: apiv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
	}
	cluster.SetInheritedDataAndOwnership(&failoverQuorum.ObjectMeta)

	err := r.Create(ctx, &failoverQuorum)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.FromContext(ctx).Error(err, "Unable to create the FailoverQuorum", "object", failoverQuorum)
		return err
	}

	return nil
}

func (r *ClusterReconciler) ensureFailoverQuorumObjectDoesNotExist(ctx context.Context, cluster *apiv1.Cluster) error {
	var failoverQuorum apiv1.FailoverQuorum

	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), &failoverQuorum); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}

		return err
	}

	return r.Delete(ctx, &failoverQuorum)
}
