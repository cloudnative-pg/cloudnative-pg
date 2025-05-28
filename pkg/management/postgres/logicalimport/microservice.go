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

// Microservice executes the microservice clone type
func Microservice(
	ctx context.Context,
	cluster *apiv1.Cluster,
	destination pool.Pooler,
	origin pool.Pooler,
) error {
	contextLogger := log.FromContext(ctx)
	ds := databaseSnapshotter{cluster: cluster}
	initDB := cluster.Spec.Bootstrap.InitDB
	databases := initDB.Import.Databases

	contextLogger.Info("starting microservice clone process")

	if err := createDumpsDirectory(); err != nil {
		return nil
	}

	if err := ds.exportDatabases(
		ctx,
		origin,
		databases,
		initDB.Import.PgDumpExtraOptions,
	); err != nil {
		return err
	}

	if err := ds.dropExtensionsFromDatabase(
		ctx,
		destination,
		initDB.Database,
	); err != nil {
		return err
	}

	if err := ds.importDatabaseContent(
		ctx,
		destination,
		databases[0],
		initDB.Database,
		initDB.Owner,
		initDB.Import.PgRestoreExtraOptions,
		initDB.Import.PgRestorePredataOptions,
		initDB.Import.PgRestoreDataOptions,
		initDB.Import.PgRestorePostdataOptions,
	); err != nil {
		return err
	}

	if err := cleanDumpDirectory(); err != nil {
		return err
	}

	if err := ds.executePostImportQueries(
		ctx,
		destination,
		initDB.Database,
	); err != nil {
		return err
	}

	return ds.analyze(ctx, destination, []string{initDB.Database})
}
