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
	"fmt"
	"os/exec"

	"github.com/lib/pq"

	"k8s.io/utils/strings/slices"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/execlog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

type databaseSnapshotter struct {
	// TODO: Only Import is required here
	cluster *v1.Cluster
}

func (ds *databaseSnapshotter) getDatabaseList(ctx context.Context, target *pool.ConnectionPool) ([]string, error) {
	contextLogger := log.FromContext(ctx)

	passedDatabases := ds.cluster.Spec.Bootstrap.InitDB.Import.Databases

	if !slices.Contains(passedDatabases, "*") {
		contextLogger.Info(
			"found an explicit database list, skipping getDatabase query",
			"databases", passedDatabases,
		)
		return passedDatabases, nil
	}

	dbPostgres, err := target.Connection(postgresDatabase)
	if err != nil {
		return nil, err
	}
	query := `SELECT datname FROM pg_database d WHERE datallowconn
              AND NOT datistemplate
              AND datallowconn
              AND datname != 'postgres'
              ORDER BY datname`

	rows, err := dbPostgres.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "while closing rows: %w")
		}
	}()

	var databases []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, err
		}
		databases = append(databases, database)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return databases, nil
}

func (ds *databaseSnapshotter) exportDatabases(
	ctx context.Context,
	target *pool.ConnectionPool,
	databases []string,
) error {
	contextLogger := log.FromContext(ctx)
	for _, database := range databases {
		contextLogger.Info("exporting database", "databaseName", database)
		dsn := target.GetDsn(database)
		options := []string{
			"-Fc",
			"-f", generateFileNameForDatabase(database),
			"-d", dsn,
			"-v",
		}

		contextLogger.Info("Running pg_dump", "cmd", pgDump,
			"options", options)
		pgDumpCommand := exec.Command(pgDump, options...) // #nosec
		err := execlog.RunStreaming(pgDumpCommand, pgDump)
		if err != nil {
			return fmt.Errorf("error in pg_dump, %w", err)
		}
	}

	return nil
}

func (ds *databaseSnapshotter) importDatabases(
	ctx context.Context,
	target *pool.ConnectionPool,
	databases []string,
) error {
	contextLogger := log.FromContext(ctx)

	for _, database := range databases {
		for _, section := range []string{"pre-data", "data", "post-data"} {
			targetDatabase := target.GetDsn(database)
			contextLogger.Info(
				"executing database importing section",
				"databaseName", database,
				"section", section,
			)

			exists, err := ds.databaseExists(target, database)
			if err != nil {
				return err
			}

			var options []string

			if !exists {
				contextLogger.Debug("database not found, creating", "databaseName", database)
				options = append(options, "--create")
				// if the database doesn't exist we need to connect to postgres
				targetDatabase = target.GetDsn(postgresDatabase)
			}

			alwaysPresentOptions := []string{
				"-U", "postgres",
				"-d", targetDatabase,
				"--section", section,
				generateFileNameForDatabase(database),
			}

			options = append(options, alwaysPresentOptions...)

			contextLogger.Info("Running pg_restore",
				"cmd", pgRestore,
				"options", options)

			pgRestoreCommand := exec.Command(pgRestore, options...) // #nosec
			err = execlog.RunStreaming(pgRestoreCommand, pgRestore)
			if err != nil {
				return fmt.Errorf("error while executing pg_restore, section:%s, %w", section, err)
			}
		}
	}

	return nil
}

func (ds *databaseSnapshotter) importDatabaseContent(
	ctx context.Context,
	target *pool.ConnectionPool,
	database string,
	targetDatabase string,
	owner string,
) error {
	contextLogger := log.FromContext(ctx)

	// We are about to execute pg_restore here.
	// That will execute "CREATE EXTENSION" and/or "COMMENT ON EXTENSION" as needed,
	// and to do that we'll generically need to be superusers on the target database.
	contextLogger.Info("temporarily granting superuser permission to owner user",
		"owner", owner)

	db, err := target.Connection(targetDatabase)
	if err != nil {
		return err
	}

	if _, err = db.Exec(fmt.Sprintf("ALTER USER %s SUPERUSER", pq.QuoteIdentifier(owner))); err != nil {
		return err
	}

	for _, section := range []string{"pre-data", "data", "post-data"} {
		contextLogger.Info(
			"executing database importing section",
			"databaseName", database,
			"section", section,
		)

		options := []string{
			"--no-owner",
			"--no-privileges",
			fmt.Sprintf("--role=%s", owner),
			"-d", targetDatabase,
			"--section", section,
			generateFileNameForDatabase(database),
		}

		contextLogger.Info("Running pg_restore",
			"cmd", pgRestore,
			"options", options)

		pgRestoreCommand := exec.Command(pgRestore, options...) // #nosec
		err = execlog.RunStreaming(pgRestoreCommand, pgRestore)
		if err != nil {
			return fmt.Errorf("error while executing pg_restore, section:%s, %w", section, err)
		}
	}

	contextLogger.Info("removing superuser permission from owner user",
		"owner", owner)
	if _, err = db.Exec(fmt.Sprintf("ALTER USER %s NOSUPERUSER", pq.QuoteIdentifier(owner))); err != nil {
		return err
	}

	return nil
}

func (ds *databaseSnapshotter) databaseExists(
	target *pool.ConnectionPool,
	dbName string,
) (bool, error) {
	db, err := target.Connection(postgresDatabase)
	if err != nil {
		return false, err
	}

	var exists bool
	row := db.QueryRow(
		"SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)",
		dbName,
	)
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (ds *databaseSnapshotter) executePostImportQueries(
	ctx context.Context,
	target *pool.ConnectionPool,
	database string,
) error {
	postImportQueries := ds.cluster.Spec.Bootstrap.InitDB.Import.PostImportApplicationSQL
	if len(postImportQueries) == 0 {
		return nil
	}

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("executing post import user defined queries")

	db, err := target.Connection(database)
	if err != nil {
		return err
	}

	for _, query := range postImportQueries {
		_, err := db.Exec(query)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ds *databaseSnapshotter) analyze(
	ctx context.Context,
	target *pool.ConnectionPool,
	databases []string,
) error {
	contextLogger := log.FromContext(ctx)

	for _, database := range databases {
		contextLogger.Info(fmt.Sprintf("running analyze for database: %s", database))
		db, err := target.Connection(database)
		if err != nil {
			return err
		}
		if _, err := db.Exec("ANALYZE VERBOSE"); err != nil {
			return err
		}
	}

	return nil
}

// dropExtensionsFromDatabase will drop every extension installed in a database.
// This is useful before restoring a backup, as the restore process will execute
// the "CREATE EXTENSION" commands that are needed
func (ds *databaseSnapshotter) dropExtensionsFromDatabase(
	ctx context.Context,
	target *pool.ConnectionPool,
	database string,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("dropping extensions from the target (empty) database")

	db, err := target.Connection(database)
	if err != nil {
		return err
	}

	rows, err := db.QueryContext(ctx, "SELECT extname FROM pg_extension")
	if err != nil {
		return err
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			contextLogger.Error(closeErr, "error closing cursor, skipped")
		}
	}()

	for rows.Next() {
		var extName string
		if err = rows.Scan(&extName); err != nil {
			return err
		}

		contextLogger.Info("dropping extension from target database", "extName", extName)
		if _, err = db.Exec(fmt.Sprintf("DROP EXTENSION %s", pq.QuoteIdentifier(extName))); err != nil {
			contextLogger.Info("cannot drop extension (this is normal for system extensions)",
				"extName", extName, "error", err)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}

	return nil
}
