/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package promote implement the kubectl-cnp promote command
package promote

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
)

// Promote command implementation
func Promote(ctx context.Context, clusterName string, serverName string) error {
	// Get the Cluster object
	cluster, err := utils.GetCluster(ctx, plugin.DynamicClient, plugin.Namespace, clusterName)
	if err != nil {
		return err
	}

	// If server name is equal to target primary, there is no need to promote
	// that instance
	if cluster.Status.TargetPrimary == serverName {
		return nil
	}

	// Check if the Pod exist
	_, err = plugin.GoClient.CoreV1().Pods(plugin.Namespace).Get(ctx, serverName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// The Pod exists, let's update status fields
	cluster.Status.TargetPrimary = serverName
	cluster.Status.Phase = apiv1.PhaseSwitchover
	cluster.Status.PhaseReason = fmt.Sprintf("Switching over to %v", serverName)

	err = utils.UpdateClusterStatus(ctx, plugin.DynamicClient, cluster)
	if err != nil {
		return err
	}

	return nil
}
