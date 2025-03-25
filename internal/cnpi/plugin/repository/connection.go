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

package repository

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/puddle/v2"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
)

// maxConnectionAttempts is the maximum number of connections attempts to a
// plugin. maxConnectionAttempts should be higher or equal to maxPoolSize
const maxConnectionAttempts = 5

type releasingConnection struct {
	connection.Interface
	closer func() error
}

func (conn *releasingConnection) Close() error {
	if conn.closer != nil {
		return conn.closer()
	}
	return conn.Interface.Close()
}

func (r *data) GetConnection(ctx context.Context, name string) (connection.Interface, error) {
	contextLogger := log.FromContext(ctx).WithValues("pluginName", name)

	pool, ok := r.pluginConnectionPool[name]
	if !ok {
		return nil, &ErrUnknownPlugin{Name: name}
	}

	// Try to get a connection from the pool and test
	// before returning it
	var resource *puddle.Resource[connection.Interface]
	var err error

	for i := 0; i < maxConnectionAttempts; i++ {
		contextLogger.Trace("try getting connection")
		resource, err = pool.Acquire(ctx)
		if err != nil {
			break
		}

		err = resource.Value().Ping(ctx)
		if err != nil {
			contextLogger.Warning(
				"Detected stale/broken plugin connection, closing and trying again",
				"pluginName", name,
				"err", err)
			resource.Destroy()
		} else {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("while getting plugin connection: %w", err)
	}

	contextLogger.Trace(
		"Acquired logical plugin connection",
		"name", name,
	)
	return &releasingConnection{
		Interface: resource.Value(),
		closer: func() error {
			contextLogger.Trace(
				"Released logical plugin connection",
				"name", name,
			)
			// When the client has done its job with a plugin connection, it
			// will be returned to the pool
			resource.Release()
			return nil
		},
	}, err
}
