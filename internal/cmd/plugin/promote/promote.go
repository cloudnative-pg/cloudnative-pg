/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package promote implement the kubectl-cnpg promote command
package promote

import (
	"context"
	"fmt"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
)

// Promote command implementation
func Promote(ctx context.Context, clusterName string, serverName string) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("cluster %s not found in namespace %s", clusterName, plugin.Namespace)
	}

	// If server name is equal to target primary, there is no need to promote
	// that instance
	if cluster.Status.TargetPrimary == serverName {
		fmt.Printf("%s is already the primary node in the cluster\n", serverName)
		return nil
	}

	// Check if the Pod exist
	var pod v1.Pod
	err = plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: serverName}, &pod)
	if err != nil {
		return fmt.Errorf("new primary node %s not found in namespace %s", serverName, plugin.Namespace)
	}

	// The Pod exists, let's update status fields
	origCluster := cluster.DeepCopy()
	cluster.Status.TargetPrimary = serverName
	cluster.Status.TargetPrimaryTimestamp = pgTime.GetCurrentTimestamp()
	if err := status.RegisterPhaseWithOrigCluster(
		ctx,
		plugin.Client,
		&cluster,
		origCluster,
		apiv1.PhaseSwitchover,
		fmt.Sprintf("Switching over to %v", serverName),
	); err != nil {
		return err
	}
	fmt.Printf("Node %s in cluster %s will be promoted\n", serverName, clusterName)
	return nil
}
