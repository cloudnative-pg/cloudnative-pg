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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// Microservice executes the microservice clone type
func Microservice(
	ctx context.Context,
	cluster *apiv1.Cluster,
	destination *pool.ConnectionPool,
	origin *pool.ConnectionPool,
) error {
	contextLogger := log.FromContext(ctx)
	ds := databaseSnapshotter{cluster: cluster}
	databases := cluster.Spec.Bootstrap.InitDB.Import.Databases
	contextLogger.Info("starting microservice clone process")

	if err := createDumpsDirectory(); err != nil {
		return nil
	}

	if err := ds.exportDatabases(ctx, origin, databases); err != nil {
		return err
	}

	if err := ds.dropExtensionsFromDatabase(ctx, destination, cluster.Spec.Bootstrap.InitDB.Database); err != nil {
		return err
	}

	if err := ds.importDatabaseContent(
		ctx,
		destination,
		databases[0],
		cluster.Spec.Bootstrap.InitDB.Database,
		cluster.Spec.Bootstrap.InitDB.Owner,
	); err != nil {
		return err
	}

	if err := cleanDumpDirectory(); err != nil {
		return err
	}

	if err := ds.executePostImportQueries(ctx, destination, cluster.Spec.Bootstrap.InitDB.Database); err != nil {
		return err
	}

	if err := ds.analyze(ctx, destination, []string{cluster.Spec.Bootstrap.InitDB.Database}); err != nil {
		return err
	}

	return nil
}
