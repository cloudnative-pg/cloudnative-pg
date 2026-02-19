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

package snapshot

import (
	"context"
	"fmt"
	"slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/status"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Enable removes a VolumeSnapshot from the cluster's ExcludedSnapshots list,
// re-enabling it for future replica creation
func Enable(ctx context.Context, cli client.Client,
	namespace, clusterName, snapshotName string,
) error {
	var cluster apiv1.Cluster

	if err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster); err != nil {
		return fmt.Errorf("cluster %s not found in namespace %s: %w", clusterName, namespace, err)
	}

	if !cluster.HasExcludedSnapshot(snapshotName) {
		fmt.Printf("Snapshot %s is not in the excluded list for cluster %s\n", snapshotName, clusterName)
		return nil
	}

	if err := status.PatchWithOptimisticLock(ctx, cli, &cluster, func(c *apiv1.Cluster) {
		c.Status.ExcludedSnapshots = slices.DeleteFunc(c.Status.ExcludedSnapshots, func(s string) bool {
			return s == snapshotName
		})
	}); err != nil {
		return err
	}

	fmt.Printf("Snapshot %s re-enabled for cluster %s\n", snapshotName, clusterName)
	return nil
}

// Disable adds a VolumeSnapshot to the cluster's ExcludedSnapshots list,
// excluding it from being used for replica creation
func Disable(ctx context.Context, cli client.Client,
	namespace, clusterName, snapshotName string,
) error {
	var cluster apiv1.Cluster

	if err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster); err != nil {
		return fmt.Errorf("cluster %s not found in namespace %s: %w", clusterName, namespace, err)
	}

	if cluster.HasExcludedSnapshot(snapshotName) {
		fmt.Printf("Snapshot %s is already excluded for cluster %s\n", snapshotName, clusterName)
		return nil
	}

	if err := status.PatchWithOptimisticLock(ctx, cli, &cluster, func(c *apiv1.Cluster) {
		c.Status.ExcludedSnapshots = append(c.Status.ExcludedSnapshots, snapshotName)
	}); err != nil {
		return err
	}

	fmt.Printf("Snapshot %s excluded for cluster %s\n", snapshotName, clusterName)
	return nil
}
