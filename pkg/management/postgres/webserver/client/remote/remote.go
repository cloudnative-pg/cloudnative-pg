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

package remote

import (
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/common"
)

// Client is the interface to interact with the remote webserver
type Client interface {
	Instance() InstanceClient
	Backup() BackupClient
}

type remoteClientImpl struct {
	instance InstanceClient
	backup   *backupClientImpl
}

func (r *remoteClientImpl) Backup() BackupClient {
	return r.backup
}

func (r *remoteClientImpl) Instance() InstanceClient {
	return r.instance
}

// NewClient creates a new remote client
func NewClient() Client {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 10 * time.Second

	return &remoteClientImpl{
		instance: &instanceClientImpl{Client: common.NewHTTPClient(connectionTimeout, requestTimeout)},
		backup:   &backupClientImpl{cli: common.NewHTTPClient(connectionTimeout, requestTimeout)},
	}
}
