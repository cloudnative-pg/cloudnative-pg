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

package logicalsnapshot

import (
	"context"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// Monolith executes the monolith clone type
func Monolith(
	ctx context.Context,
	cluster *v1.Cluster,
	destination *pool.ConnectionPool,
	origin *pool.ConnectionPool,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("starting monolith clone process")

	if err := cloneRoles(ctx, cluster, destination, origin); err != nil {
		return err
	}

	if err := cloneRoleInheritance(ctx, destination, origin); err != nil {
		return err
	}

	ds := databaseSnapshotter{cluster: cluster}
	databases, err := ds.getDatabaseList(ctx, origin)
	if err != nil {
		return err
	}

	if err := createDumpsDirectory(); err != nil {
		return err
	}

	if err := ds.exportDatabases(ctx, origin, databases); err != nil {
		return err
	}

	if err := ds.importDatabases(ctx, destination, databases); err != nil {
		return err
	}

	if err := cleanDumpDirectory(); err != nil {
		return err
	}

	if err := ds.analyze(ctx, destination, databases); err != nil {
		return err
	}

	return nil
}
