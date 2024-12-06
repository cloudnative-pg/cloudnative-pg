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

package local

import (
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/common"
)

// Client is an entity capable of interacting with the local webserver endpoints
type Client interface {
	Backup() BackupClient
	Cache() CacheClient
	Cluster() ClusterClient
}

type localClient struct {
	backup  BackupClient
	cache   CacheClient
	cluster ClusterClient
}

// NewClient returns a new instance of Client
func NewClient() Client {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	standardClient := common.NewHTTPClient(connectionTimeout, requestTimeout)

	return &localClient{
		backup:  &backupClientImpl{cli: standardClient},
		cache:   &cacheClientImpl{cli: standardClient},
		cluster: &clusterClientImpl{cli: standardClient},
	}
}

func (c *localClient) Backup() BackupClient {
	return c.backup
}

func (c *localClient) Cache() CacheClient {
	return c.cache
}

func (c *localClient) Cluster() ClusterClient {
	return c.cluster
}
