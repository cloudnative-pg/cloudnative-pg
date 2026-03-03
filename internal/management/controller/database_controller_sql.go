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

package controller

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

func detectDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) (bool, error) {
	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_catalog.pg_database
	        WHERE datname = $1
		`,
		obj.Spec.Name)
	if row.Err() != nil {
		return false, fmt.Errorf("while checking if database %q exists: %w", obj.Spec.Name, row.Err())
	}

	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("while scanning if database %q exists: %w", obj.Spec.Name, err)
	}

	return count > 0, nil
}

func createDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateDatabase strings.Builder
	fmt.Fprintf(&sqlCreateDatabase, "CREATE DATABASE %s ", pgx.Identifier{obj.Spec.Name}.Sanitize())
	if len(obj.Spec.Owner) > 0 {
		fmt.Fprintf(&sqlCreateDatabase, " OWNER %s", pgx.Identifier{obj.Spec.Owner}.Sanitize())
	}
	if len(obj.Spec.Template) > 0 {
		fmt.Fprintf(&sqlCreateDatabase, " TEMPLATE %s", pgx.Identifier{obj.Spec.Template}.Sanitize())
	}
	if len(obj.Spec.Tablespace) > 0 {
		fmt.Fprintf(&sqlCreateDatabase, " TABLESPACE %s", pgx.Identifier{obj.Spec.Tablespace}.Sanitize())
	}
	if obj.Spec.AllowConnections != nil {
		fmt.Fprintf(&sqlCreateDatabase, " ALLOW_CONNECTIONS %v", *obj.Spec.AllowConnections)
	}
	if obj.Spec.ConnectionLimit != nil {
		fmt.Fprintf(&sqlCreateDatabase, " CONNECTION LIMIT %v", *obj.Spec.ConnectionLimit)
	}
	if obj.Spec.IsTemplate != nil {
		fmt.Fprintf(&sqlCreateDatabase, " IS_TEMPLATE %v", *obj.Spec.IsTemplate)
	}
	if obj.Spec.Encoding != "" {
		fmt.Fprintf(&sqlCreateDatabase, " ENCODING %s", pgx.Identifier{obj.Spec.Encoding}.Sanitize())
	}
	if obj.Spec.Locale != "" {
		fmt.Fprintf(&sqlCreateDatabase, " LOCALE %s", pgx.Identifier{obj.Spec.Locale}.Sanitize())
	}
	if obj.Spec.LocaleProvider != "" {
		fmt.Fprintf(&sqlCreateDatabase, " LOCALE_PROVIDER %s",
			pgx.Identifier{obj.Spec.LocaleProvider}.Sanitize())
	}
	if obj.Spec.LcCollate != "" {
		fmt.Fprintf(&sqlCreateDatabase, " LC_COLLATE %s", pgx.Identifier{obj.Spec.LcCollate}.Sanitize())
	}
	if obj.Spec.LcCtype != "" {
		fmt.Fprintf(&sqlCreateDatabase, " LC_CTYPE %s", pgx.Identifier{obj.Spec.LcCtype}.Sanitize())
	}
	if obj.Spec.IcuLocale != "" {
		fmt.Fprintf(&sqlCreateDatabase, " ICU_LOCALE %s", pgx.Identifier{obj.Spec.IcuLocale}.Sanitize())
	}
	if obj.Spec.IcuRules != "" {
		fmt.Fprintf(&sqlCreateDatabase, " ICU_RULES %s", pgx.Identifier{obj.Spec.IcuRules}.Sanitize())
	}
	if obj.Spec.BuiltinLocale != "" {
		fmt.Fprintf(&sqlCreateDatabase, " BUILTIN_LOCALE %s",
			pgx.Identifier{obj.Spec.BuiltinLocale}.Sanitize())
	}
	if obj.Spec.CollationVersion != "" {
		fmt.Fprintf(&sqlCreateDatabase, " COLLATION_VERSION %s",
			pgx.Identifier{obj.Spec.CollationVersion}.Sanitize())
	}

	_, err := db.ExecContext(ctx, sqlCreateDatabase.String())
	if err != nil {
		contextLogger.Error(err, "while creating database", "query", sqlCreateDatabase.String())
	}

	if err != nil {
		return fmt.Errorf("while creating database %q: %w",
			obj.Spec.Name, err)
	}
	return nil
}

func updateDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) error {
	contextLogger := log.FromContext(ctx)

	if obj.Spec.AllowConnections != nil {
		changeAllowConnectionsSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH ALLOW_CONNECTIONS %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.AllowConnections)

		if _, err := db.ExecContext(ctx, changeAllowConnectionsSQL); err != nil {
			contextLogger.Error(err, "while altering database", "query", changeAllowConnectionsSQL)
			return fmt.Errorf("while altering database %q with allow_connections %t: %w",
				obj.Spec.Name, *obj.Spec.AllowConnections, err)
		}
	}

	if obj.Spec.ConnectionLimit != nil {
		changeConnectionsLimitSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH CONNECTION LIMIT %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.ConnectionLimit)

		if _, err := db.ExecContext(ctx, changeConnectionsLimitSQL); err != nil {
			contextLogger.Error(err, "while altering database", "query", changeConnectionsLimitSQL)
			return fmt.Errorf("while altering database %q with connection limit %d: %w",
				obj.Spec.Name, *obj.Spec.ConnectionLimit, err)
		}
	}

	if obj.Spec.IsTemplate != nil {
		changeIsTemplateSQL := fmt.Sprintf(
			"ALTER DATABASE %s WITH IS_TEMPLATE %v",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			*obj.Spec.IsTemplate)

		if _, err := db.ExecContext(ctx, changeIsTemplateSQL); err != nil {
			contextLogger.Error(err, "while altering database", "query", changeIsTemplateSQL)
			return fmt.Errorf("while altering database %q with is_template %t: %w",
				obj.Spec.Name, *obj.Spec.IsTemplate, err)
		}
	}

	if len(obj.Spec.Owner) > 0 {
		changeOwnerSQL := fmt.Sprintf(
			"ALTER DATABASE %s OWNER TO %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			pgx.Identifier{obj.Spec.Owner}.Sanitize())

		if _, err := db.ExecContext(ctx, changeOwnerSQL); err != nil {
			contextLogger.Error(err, "while altering database", "query", changeOwnerSQL)
			return fmt.Errorf("while altering database %q owner to %s: %w",
				obj.Spec.Name, obj.Spec.Owner, err)
		}
	}

	if len(obj.Spec.Tablespace) > 0 {
		changeTablespaceSQL := fmt.Sprintf(
			"ALTER DATABASE %s SET TABLESPACE %s",
			pgx.Identifier{obj.Spec.Name}.Sanitize(),
			pgx.Identifier{obj.Spec.Tablespace}.Sanitize())

		if _, err := db.ExecContext(ctx, changeTablespaceSQL); err != nil {
			contextLogger.Error(err, "while altering database", "query", changeTablespaceSQL)
			return fmt.Errorf("while altering database %q tablespace to %s: %w",
				obj.Spec.Name, obj.Spec.Tablespace, err)
		}
	}

	return nil
}

func dropDatabase(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Database,
) error {
	contextLogger := log.FromContext(ctx)
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{obj.Spec.Name}.Sanitize())
	_, err := db.ExecContext(
		ctx,
		query)
	if err != nil {
		contextLogger.Error(err, "while dropping database", "query", query)
		return fmt.Errorf("while dropping database %q: %w", obj.Spec.Name, err)
	}

	return nil
}
