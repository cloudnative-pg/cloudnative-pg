/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package restart implements a command to rollout restart a cluster
package restart

import (
	"context"
	"fmt"
	"time"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	mutils "github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// Restart marks the cluster as needing to restart
func Restart(ctx context.Context, clusterName string) error {
	// Get the Cluster object
	cluster, err := mutils.GetCluster(ctx, plugin.DynamicClient, plugin.Namespace, clusterName)
	if err != nil {
		return err
	}

	clusterRestarted := cluster.DeepCopy()
	clusterRestarted.Annotations[specs.ClusterRestartAnnotationName] = time.Now().Format(time.RFC3339)
	clusterRestarted.ManagedFields = nil
	err = mutils.PatchCluster(ctx, plugin.DynamicClient, clusterRestarted)
	if err != nil {
		return err
	}

	fmt.Printf("%s restarted", clusterRestarted.Name)
	return nil
}
