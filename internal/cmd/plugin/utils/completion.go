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

package utils

import (
	"context"
	"strings"

	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// CompleteClusters will complete the cluster name when necessary getting the list from the current namespace
func CompleteClusters(ctx context.Context, cli client.Client, args []string, toComplete string) []string {
	var clusters apiv1.ClusterList

	// Get the cluster lists object if error we just return empty array string
	if err := cli.List(ctx, &clusters, client.InNamespace(plugin.Namespace)); err != nil {
		return []string{}
	}

	clustersNames := make([]string, 0, len(clusters.Items))
	for _, cluster := range clusters.Items {
		if (len(toComplete) > 0 && !strings.HasPrefix(cluster.Name, toComplete)) ||
			slices.Contains(args, cluster.Name) {
			continue
		}

		clustersNames = append(clustersNames, cluster.Name)
	}

	return clustersNames
}
