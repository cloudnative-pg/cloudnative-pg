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
	"k8s.io/apimachinery/pkg/runtime/schema"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// Promote command implementation
func Promote(ctx context.Context, clusterName string, serverName string) {
	// Check cluster status
	cluster, err := utils.GetCluster(ctx, plugin.DynamicClient, plugin.Namespace, clusterName)
	if err != nil {
		log.Log.Error(err, "Cannot find PostgreSQL cluster",
			"namespace", plugin.Namespace,
			"name", clusterName)
		return
	}

	// Check if the Pod exist
	_, err = plugin.DynamicClient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}).Namespace(plugin.Namespace).Get(ctx, serverName, metav1.GetOptions{})
	if err != nil {
		log.Log.Error(err, "Cannot find PostgreSQL server",
			"namespace", plugin.Namespace,
			"name", serverName)
		return
	}

	// The Pod exists, let's do it!
	cluster.Status.TargetPrimary = serverName
	cluster.Status.Phase = apiv1.PhaseSwitchover
	cluster.Status.PhaseReason = fmt.Sprintf("Switching over to %v", serverName)

	err = utils.UpdateClusterStatus(ctx, plugin.DynamicClient, cluster)
	if err != nil {
		log.Log.Error(err, "Cannot update PostgreSQL cluster status",
			"namespace", plugin.Namespace,
			"name", serverName,
			"cluster", cluster)
		return
	}
}
