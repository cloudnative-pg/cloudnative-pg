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
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ClusterCache provides a resilient way to fetch cluster definitions with caching
// to handle transient API server connectivity issues.
// This cache is thread-safe and can be shared across multiple probe types.
type ClusterCache struct {
	cli     client.Client
	key     client.ObjectKey
	timeout time.Duration

	mu                 sync.RWMutex
	latestKnownCluster *apiv1.Cluster
}

// NewClusterCache creates a new cluster cache instance that can be shared across multiple probes
func NewClusterCache(cli client.Client, key client.ObjectKey) *ClusterCache {
	return &ClusterCache{
		cli: cli,
		key: key,
		// We set a safe context timeout of 500ms to avoid a failed request from taking
		// more time than the minimum configurable timeout (1s) of the container's probe,
		// which otherwise could have triggered a restart of the instance.
		timeout: 500 * time.Millisecond,
	}
}

// tryGetLatestClusterWithTimeout attempts to fetch a fresh cluster definition with a timeout.
// Writes the cluster data into the provided output parameter.
// Returns nil on success, or an error on failure (falling back to cached value if available).
func (c *ClusterCache) tryGetLatestClusterWithTimeout(ctx context.Context, out *apiv1.Cluster) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	err := c.cli.Get(timeoutCtx, c.key, out)
	if err != nil {
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.latestKnownCluster != nil {
			c.latestKnownCluster.DeepCopyInto(out)
		}
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latestKnownCluster == nil {
		c.latestKnownCluster = &apiv1.Cluster{}
	}
	out.DeepCopyInto(c.latestKnownCluster)
	return nil
}
