/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package fence implements a command to fence instances in a cluster
package fence

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// fencingOn marks an instance in a cluster as fenced
func fencingOn(ctx context.Context, clusterName string, serverName string) error {
	err := ApplyFenceFunc(ctx, plugin.Client, clusterName, plugin.Namespace, serverName, utils.AddFencedInstance)
	if err != nil {
		return err
	}
	fmt.Printf("%s fenced\n", serverName)
	return nil
}

// fencingOff marks an instance in a cluster as not fenced
func fencingOff(ctx context.Context, clusterName string, serverName string) error {
	err := ApplyFenceFunc(ctx, plugin.Client, clusterName, plugin.Namespace, serverName, utils.RemoveFencedInstance)
	if err != nil {
		return err
	}
	fmt.Printf("%s unfenced\n", serverName)
	return nil
}

// ApplyFenceFunc applies a given fencing function to a cluster in a namespace
func ApplyFenceFunc(
	ctx context.Context,
	cli client.Client,
	clusterName string,
	namespace string,
	serverName string,
	fenceFunc func(string, map[string]string) error,
) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster)
	if err != nil {
		return err
	}

	if serverName != utils.FenceAllServers {
		// Check if the Pod exist
		var pod v1.Pod
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serverName}, &pod)
		if err != nil {
			return fmt.Errorf("node %s not found in namespace %s", serverName, namespace)
		}
	}

	fencedCluster := cluster.DeepCopy()

	if err = fenceFunc(serverName, fencedCluster.Annotations); err != nil {
		return err
	}
	fencedCluster.ManagedFields = nil

	err = cli.Patch(ctx, fencedCluster, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}

	return nil
}
