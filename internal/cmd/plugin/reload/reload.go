/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package reload implements a command to trigger a reconciliation loop for a cluster
package reload

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// Reload marks the cluster as needing to have a reconciliation loop
func Reload(ctx context.Context, clusterName string) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return err
	}

	clusterRestarted := cluster.DeepCopy()
	if clusterRestarted.Annotations == nil {
		clusterRestarted.Annotations = make(map[string]string)
	}
	clusterRestarted.Annotations[specs.ClusterReloadAnnotationName] = time.Now().Format(time.RFC3339)
	clusterRestarted.ManagedFields = nil

	err = plugin.Client.Patch(ctx, clusterRestarted, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}

	fmt.Printf("%s will be reloaded\n", clusterRestarted.Name)
	return nil
}
