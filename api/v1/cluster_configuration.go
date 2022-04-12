/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package v1

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// GetSyncReplicasNumber computes the actual number of required synchronous replicas
// given the requested min, max and the number of ready replicas in the cluster
func (cluster *Cluster) GetSyncReplicasNumber() (syncReplicas int) {
	// We start with the number of healthy replicas (healthy pods minus one)
	// and verify it is greater than 0 and between minSyncReplicas and maxSyncReplicas.
	// Formula: 1 <= minSyncReplicas <= SyncReplicas <= maxSyncReplicas < readyReplicas
	readyReplicas := len(cluster.Status.InstancesStatus[utils.PodHealthy]) - 1

	// Initially set it to the max sync replicas requested by user
	syncReplicas = int(cluster.Spec.MaxSyncReplicas)

	// Lower to min sync replicas if not enough ready replicas
	if readyReplicas < syncReplicas {
		syncReplicas = int(cluster.Spec.MinSyncReplicas)
	}

	// Lower to ready replicas if min sync replicas is too high
	// (this is a self-healing procedure that prevents from a
	// temporarily unresponsive system)
	if readyReplicas < int(cluster.Spec.MinSyncReplicas) {
		syncReplicas = readyReplicas
		log.Info("Ignore minSyncReplicas to enforce self-healing",
			"syncReplicas", readyReplicas,
			"minSyncReplicas", cluster.Spec.MinSyncReplicas,
			"maxSyncReplicas", cluster.Spec.MaxSyncReplicas)
	}

	return syncReplicas
}
