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

package logicalimport

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// Monolith executes the monolith clone type
func Monolith(
	ctx context.Context,
	cluster *apiv1.Cluster,
	destination pool.Pooler,
	origin pool.Pooler,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("starting monolith clone process")

	if len(cluster.Spec.Bootstrap.InitDB.Import.Roles) > 0 {
		if err := cloneRoles(ctx, cluster, destination, origin); err != nil {
			return err
		}
		if err := cloneRoleInheritance(ctx, destination, origin); err != nil {
			return err
		}
	}

	ds := databaseSnapshotter{cluster: cluster}
	databases, err := ds.getDatabaseList(ctx, origin)
	if err != nil {
		return err
	}

	if err := createDumpsDirectory(); err != nil {
		return err
	}

	if err := ds.exportDatabases(
		ctx,
		origin,
		databases,
		cluster.Spec.Bootstrap.InitDB.Import.PgDumpExtraOptions,
	); err != nil {
		return err
	}

	if err := ds.importDatabases(
		ctx,
		destination,
		databases,
		cluster.Spec.Bootstrap.InitDB.Import.PgRestoreExtraOptions,
		cluster.Spec.Bootstrap.InitDB.Import.PgRestorePredataOptions,
		cluster.Spec.Bootstrap.InitDB.Import.PgRestoreDataOptions,
		cluster.Spec.Bootstrap.InitDB.Import.PgRestorePostdataOptions,
	); err != nil {
		return err
	}

	if err := cleanDumpDirectory(); err != nil {
		return err
	}

	return ds.analyze(ctx, destination, databases)
}
