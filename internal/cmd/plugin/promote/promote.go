/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package promote implement the kubectl-cnp promote command
package promote

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

// Promote command implementation
func Promote(ctx context.Context, clusterName string, serverName string) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return err
	}

	// If server name is equal to target primary, there is no need to promote
	// that instance
	if cluster.Status.TargetPrimary == serverName {
		return nil
	}

	// Check if the Pod exist
	var pod v1.Pod
	err = plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: serverName}, &pod)
	if err != nil {
		return err
	}

	// The Pod exists, let's update status fields
	cluster.Status.TargetPrimary = serverName
	cluster.Status.Phase = apiv1.PhaseSwitchover
	cluster.Status.PhaseReason = fmt.Sprintf("Switching over to %v", serverName)

	err = plugin.Client.Status().Update(ctx, &cluster)
	if err != nil {
		return err
	}

	return nil
}
