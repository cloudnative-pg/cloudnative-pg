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
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/puddle/v2"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
)

// connectionBackoff is the retry policy used to get a plugin
// connection from a pool.
var connectionBackoff = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
}

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

	var resource *puddle.Resource[connection.Interface]

	if err := retry.OnError(connectionBackoff, func(_ error) bool { return true }, func() error {
		pool, ok := r.pluginConnectionPool[name]
		if !ok {
			return &ErrUnknownPlugin{Name: name}
		}

		// Try to get a connection from the pool and test
		// before returning it
		var err error

		contextLogger.Trace("try getting connection")
		resource, err = pool.Acquire(ctx)
		if err != nil {
			return err
		}

		err = resource.Value().Ping(ctx)
		if err != nil {
			contextLogger.Warning(
				"Detected stale/broken plugin connection, closing and trying again",
				"pluginName", name,
				"err", err)
			resource.Destroy()
			return err
		}

		return nil
	}); err != nil {
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
	}, nil
}
