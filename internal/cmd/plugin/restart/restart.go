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

// Package restart implements a command to rollout restart a cluster or restart a single instance
package restart

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// restart marks the cluster as needing to restart
func restart(ctx context.Context, clusterName string) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("while trying to get cluster %v: %w", clusterName, err)
	}

	clusterRestarted := cluster.DeepCopy()
	if clusterRestarted.Annotations == nil {
		clusterRestarted.Annotations = make(map[string]string)
	}
	clusterRestarted.Annotations[utils.ClusterRestartAnnotationName] = time.Now().Format(time.RFC3339)
	clusterRestarted.ManagedFields = nil

	err = plugin.Client.Patch(ctx, clusterRestarted, client.MergeFrom(&cluster))
	if err != nil {
		return fmt.Errorf("while patching cluster %v: %w", clusterName, err)
	}

	fmt.Printf("%s restarted\n", clusterRestarted.Name)
	return nil
}

// instanceRestart restarts a given instance, in-place if a primary, deleting the pod if it's a replica
func instanceRestart(ctx context.Context, clusterName, node string) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return err
	}

	if cluster.Status.CurrentPrimary == node {
		if err := status.PatchWithOptimisticLock(
			ctx,
			plugin.Client,
			&cluster,
			status.SetPhase(apiv1.PhaseInplacePrimaryRestart, "Requested by the user"),
			status.SetClusterReadyCondition,
		); err != nil {
			return fmt.Errorf("while requesting restart on primary POD for cluster %v: %w", clusterName, err)
		}
	} else {
		var pod corev1.Pod
		err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: node}, &pod)
		if err != nil {
			return fmt.Errorf("while getting POD %v: %w", node, err)
		}
		if err := plugin.Client.Delete(ctx, &pod); err != nil {
			return fmt.Errorf("while deleting POD %v: %w", node, err)
		}
	}
	fmt.Printf("instance %s restarted\n", node)
	return nil
}
