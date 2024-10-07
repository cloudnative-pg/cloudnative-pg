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

// Package reload implements a command to trigger a reconciliation loop for a cluster
package reload

import (
	"context"
	"fmt"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
	clusterRestarted.Annotations[utils.ClusterReloadAnnotationName] = pgTime.GetCurrentTimestamp()
	clusterRestarted.ManagedFields = nil

	err = plugin.Client.Patch(ctx, clusterRestarted, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}

	fmt.Printf("%s will be reloaded\n", clusterRestarted.Name)
	return nil
}
