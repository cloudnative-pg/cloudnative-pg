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

package probes

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// clusterCache provides a resilient way to fetch cluster definitions with caching
// to handle transient API server connectivity issues
type clusterCache struct {
	cli     client.Client
	key     client.ObjectKey
	timeout time.Duration

	latestKnownCluster *apiv1.Cluster
}

// newClusterCache creates a new cluster cache instance
func newClusterCache(cli client.Client, key client.ObjectKey) *clusterCache {
	return &clusterCache{
		cli: cli,
		key: key,
		// We set a safe context timeout of 500ms to avoid a failed request from taking
		// more time than the minimum configurable timeout (1s) of the container's probe,
		// which otherwise could have triggered a restart of the instance.
		timeout: 500 * time.Millisecond,
	}
}

// tryRefreshLatestClusterWithTimeout refreshes the latest cluster definition with a timeout,
// returns a bool indicating if the operation was successful
func (c *clusterCache) tryRefreshLatestClusterWithTimeout(ctx context.Context) bool {
	timeoutContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var cluster apiv1.Cluster
	err := c.cli.Get(timeoutContext, c.key, &cluster)
	if err != nil {
		return false
	}

	c.latestKnownCluster = cluster.DeepCopy()
	return true
}

// getLatestKnownCluster returns the latest known cluster definition, or nil if none is available
func (c *clusterCache) getLatestKnownCluster() *apiv1.Cluster {
	return c.latestKnownCluster
}
