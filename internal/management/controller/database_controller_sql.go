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
	"errors"
	"fmt"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

type extInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Schema  string `json:"schema"`
}

type schemaInfo struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type fdwInfo struct {
	Name      string                           `json:"name"`
	Handler   string                           `json:"handler"`
	Validator string                           `json:"validator"`
	Owner     string                           `json:"owner"`
	Options   map[string]apiv1.OptionSpecValue `json:"options"`
}

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
	sqlCreateDatabase.WriteString(fmt.Sprintf("CREATE DATABASE %s ", pgx.Identifier{obj.Spec.Name}.Sanitize()))
	if len(obj.Spec.Owner) > 0 {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" OWNER %s", pgx.Identifier{obj.Spec.Owner}.Sanitize()))
	}
	if len(obj.Spec.Template) > 0 {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" TEMPLATE %s", pgx.Identifier{obj.Spec.Template}.Sanitize()))
	}
	if len(obj.Spec.Tablespace) > 0 {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" TABLESPACE %s", pgx.Identifier{obj.Spec.Tablespace}.Sanitize()))
	}
	if obj.Spec.AllowConnections != nil {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" ALLOW_CONNECTIONS %v", *obj.Spec.AllowConnections))
	}
	if obj.Spec.ConnectionLimit != nil {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" CONNECTION LIMIT %v", *obj.Spec.ConnectionLimit))
	}
	if obj.Spec.IsTemplate != nil {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" IS_TEMPLATE %v", *obj.Spec.IsTemplate))
	}
	if obj.Spec.Encoding != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" ENCODING %s", pgx.Identifier{obj.Spec.Encoding}.Sanitize()))
	}
	if obj.Spec.Locale != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" LOCALE %s", pgx.Identifier{obj.Spec.Locale}.Sanitize()))
	}
	if obj.Spec.LocaleProvider != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" LOCALE_PROVIDER %s",
			pgx.Identifier{obj.Spec.LocaleProvider}.Sanitize()))
	}
	if obj.Spec.LcCollate != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" LC_COLLATE %s", pgx.Identifier{obj.Spec.LcCollate}.Sanitize()))
	}
	if obj.Spec.LcCtype != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" LC_CTYPE %s", pgx.Identifier{obj.Spec.LcCtype}.Sanitize()))
	}
	if obj.Spec.IcuLocale != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" ICU_LOCALE %s", pgx.Identifier{obj.Spec.IcuLocale}.Sanitize()))
	}
	if obj.Spec.IcuRules != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" ICU_RULES %s", pgx.Identifier{obj.Spec.IcuRules}.Sanitize()))
	}
	if obj.Spec.BuiltinLocale != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" BUILTIN_LOCALE %s",
			pgx.Identifier{obj.Spec.BuiltinLocale}.Sanitize()))
	}
	if obj.Spec.CollationVersion != "" {
		sqlCreateDatabase.WriteString(fmt.Sprintf(" COLLATION_VERSION %s",
			pgx.Identifier{obj.Spec.CollationVersion}.Sanitize()))
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

const detectDatabaseExtensionSQL = `
SELECT e.extname, e.extversion, n.nspname
FROM pg_catalog.pg_extension e
JOIN pg_catalog.pg_namespace n ON e.extnamespace=n.oid
WHERE e.extname = $1
`

func getDatabaseExtensionInfo(ctx context.Context, db *sql.DB, ext apiv1.ExtensionSpec) (*extInfo, error) {
	row := db.QueryRowContext(
		ctx, detectDatabaseExtensionSQL,
		ext.Name)
	if row.Err() != nil {
		return nil, fmt.Errorf("while checking if extension %q exists: %w", ext.Name, row.Err())
	}

	var result extInfo
	if err := row.Scan(&result.Name, &result.Version, &result.Schema); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("while scanning if extension %q exists: %w", ext.Name, err)
	}

	return &result, nil
}

func createDatabaseExtension(ctx context.Context, db *sql.DB, ext apiv1.ExtensionSpec) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateExtension strings.Builder
	sqlCreateExtension.WriteString(fmt.Sprintf("CREATE EXTENSION %s ", pgx.Identifier{ext.Name}.Sanitize()))
	if len(ext.Version) > 0 {
		sqlCreateExtension.WriteString(fmt.Sprintf(" VERSION %s", pgx.Identifier{ext.Version}.Sanitize()))
	}
	if len(ext.Schema) > 0 {
		sqlCreateExtension.WriteString(fmt.Sprintf(" SCHEMA %s", pgx.Identifier{ext.Schema}.Sanitize()))
	}

	_, err := db.ExecContext(ctx, sqlCreateExtension.String())
	if err != nil {
		contextLogger.Error(err, "while creating extension", "query", sqlCreateExtension.String())
		return err
	}
	contextLogger.Info("created extension", "name", ext.Name)

	return nil
}

func dropDatabaseExtension(ctx context.Context, db *sql.DB, ext apiv1.ExtensionSpec) error {
	contextLogger := log.FromContext(ctx)
	query := fmt.Sprintf("DROP EXTENSION IF EXISTS %s", pgx.Identifier{ext.Name}.Sanitize())
	_, err := db.ExecContext(
		ctx,
		query)
	if err != nil {
		contextLogger.Error(err, "while dropping extension", "query", query)
		return err
	}
	contextLogger.Info("dropped extension", "name", ext.Name)
	return nil
}

func updateDatabaseExtension(ctx context.Context, db *sql.DB, spec apiv1.ExtensionSpec, info *extInfo) error {
	contextLogger := log.FromContext(ctx)
	if len(spec.Schema) > 0 && spec.Schema != info.Schema {
		changeSchemaSQL := fmt.Sprintf(
			"ALTER EXTENSION %s SET SCHEMA %v",
			pgx.Identifier{spec.Name}.Sanitize(),
			pgx.Identifier{spec.Schema}.Sanitize(),
		)

		if _, err := db.ExecContext(ctx, changeSchemaSQL); err != nil {
			return fmt.Errorf("altering schema: %w", err)
		}

		contextLogger.Info("altered extension schema", "name", spec.Name, "schema", spec.Schema)
	}

	if len(spec.Version) > 0 && spec.Version != info.Version {
		//nolint:gosec
		changeVersionSQL := fmt.Sprintf(
			"ALTER EXTENSION %s UPDATE TO %v",
			pgx.Identifier{spec.Name}.Sanitize(),
			pgx.Identifier{spec.Version}.Sanitize(),
		)

		if _, err := db.ExecContext(ctx, changeVersionSQL); err != nil {
			return fmt.Errorf("altering version: %w", err)
		}

		contextLogger.Info("altered extension version", "name", spec.Name, "version", spec.Version)
	}

	return nil
}

const detectDatabaseSchemaSQL = `
SELECT n.nspname, a.rolname
FROM pg_catalog.pg_namespace n
JOIN pg_catalog.pg_authid a ON n.nspowner = a.oid
WHERE n.nspname = $1
`

func getDatabaseSchemaInfo(ctx context.Context, db *sql.DB, schema apiv1.SchemaSpec) (*schemaInfo, error) {
	row := db.QueryRowContext(
		ctx, detectDatabaseSchemaSQL,
		schema.Name)
	if row.Err() != nil {
		return nil, fmt.Errorf("while checking if schema %q exists: %w", schema.Name, row.Err())
	}

	var result schemaInfo
	if err := row.Scan(&result.Name, &result.Owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("while scanning if schema %q exists: %w", schema.Name, err)
	}

	return &result, nil
}

func createDatabaseSchema(ctx context.Context, db *sql.DB, schema apiv1.SchemaSpec) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateExtension strings.Builder
	sqlCreateExtension.WriteString(fmt.Sprintf("CREATE SCHEMA %s ", pgx.Identifier{schema.Name}.Sanitize()))
	if len(schema.Owner) > 0 {
		sqlCreateExtension.WriteString(fmt.Sprintf(" AUTHORIZATION %s", pgx.Identifier{schema.Owner}.Sanitize()))
	}

	_, err := db.ExecContext(ctx, sqlCreateExtension.String())
	if err != nil {
		contextLogger.Error(err, "while creating schema", "query", sqlCreateExtension.String())
		return err
	}
	contextLogger.Info("created schema", "name", schema.Name)

	return nil
}

func updateDatabaseSchema(ctx context.Context, db *sql.DB, schema apiv1.SchemaSpec, info *schemaInfo) error {
	contextLogger := log.FromContext(ctx)
	if len(schema.Owner) > 0 && schema.Owner != info.Owner {
		changeSchemaSQL := fmt.Sprintf(
			"ALTER SCHEMA %s OWNER TO %v",
			pgx.Identifier{schema.Name}.Sanitize(),
			pgx.Identifier{schema.Owner}.Sanitize(),
		)

		if _, err := db.ExecContext(ctx, changeSchemaSQL); err != nil {
			return fmt.Errorf("altering schema: %w", err)
		}

		contextLogger.Info("altered schema owner", "name", schema.Name, "owner", schema.Owner)
	}

	return nil
}

func dropDatabaseSchema(ctx context.Context, db *sql.DB, schema apiv1.SchemaSpec) error {
	contextLogger := log.FromContext(ctx)
	query := fmt.Sprintf("DROP SCHEMA IF EXISTS %s", pgx.Identifier{schema.Name}.Sanitize())
	_, err := db.ExecContext(
		ctx,
		query)
	if err != nil {
		contextLogger.Error(err, "while dropping schema", "query", query)
		return err
	}
	contextLogger.Info("dropped schema", "name", schema.Name)
	return nil
}

const detectDatabaseFDWSQL = `
SELECT
 fdwname, fdwhandler::regproc::text, fdwvalidator::regproc::text, fdwoptions,
 a.rolname AS owner
FROM pg_foreign_data_wrapper f
JOIN pg_authid a ON f.fdwowner = a.oid
WHERE fdwname = $1
`

func getDatabaseFDWInfo(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec) (*fdwInfo, error) {
	row := db.QueryRowContext(
		ctx, detectDatabaseFDWSQL,
		fdw.Name)
	if row.Err() != nil {
		return nil, fmt.Errorf("while checking if FDW %q exists: %w", fdw.Name, row.Err())
	}

	var (
		result     fdwInfo
		optionsRaw pq.StringArray
	)

	if err := row.Scan(&result.Name, &result.Handler, &result.Validator, &optionsRaw, &result.Owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("while scanning if FDW %q exists: %w", fdw.Name, err)
	}

	// Extract options from SQL raw format(e.g. -{host=localhost,port=5432}) to type OptSpec
	opts := make(map[string]apiv1.OptionSpecValue, len(optionsRaw))
	for _, opt := range optionsRaw {
		parts := strings.SplitN(opt, "=", 2)
		if len(parts) == 2 {
			opts[parts[0]] = apiv1.OptionSpecValue{
				Value: parts[1],
			}
		}
	}
	result.Options = opts

	return &result, nil
}

func createDatabaseFDW(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateFDW strings.Builder
	sqlCreateFDW.WriteString(fmt.Sprintf("CREATE FOREIGN DATA WRAPPER %s ", pgx.Identifier{fdw.Name}.Sanitize()))

	// Extract handler and validator
	// If no handler or validator is provided, the default handler or validator will be used
	if fdw.Handler != nil {
		// Handler is set to ""
		if len(*fdw.Handler) == 0 {
			sqlCreateFDW.WriteString("NO HANDLER ")
		} else {
			sqlCreateFDW.WriteString(fmt.Sprintf("HANDLER %s ", pgx.Identifier{*fdw.Handler}.Sanitize()))
		}
	}
	if fdw.Validator != nil {
		// Validator is set to ""
		if len(*fdw.Validator) == 0 {
			sqlCreateFDW.WriteString("NO VALIDATOR ")
		} else {
			sqlCreateFDW.WriteString(fmt.Sprintf("VALIDATOR %s ", pgx.Identifier{*fdw.Validator}.Sanitize()))
		}
	}

	// Extract options
	opts := make([]string, 0, len(fdw.Options))
	for name, optionSpec := range fdw.Options {
		if optionSpec.Ensure == apiv1.EnsureAbsent {
			continue
		}
		opts = append(opts, fmt.Sprintf("%s '%s'", pgx.Identifier{name}.Sanitize(),
			optionSpec.Value))
	}
	if len(opts) > 0 {
		sqlCreateFDW.WriteString("OPTIONS (" + strings.Join(opts, ", ") + ")")
	}

	_, err := db.ExecContext(ctx, sqlCreateFDW.String())
	if err != nil {
		contextLogger.Error(err, "while creating foreign data wrapper", "query", sqlCreateFDW.String())
		return err
	}
	contextLogger.Info("created foreign data wrapper", "name", fdw.Name)

	return nil
}

func updateFDWOptions(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec, info *fdwInfo) error {
	contextLogger := log.FromContext(ctx)

	// Collect individual ALTER-clauses
	var clauses []string
	for name, desiredOptSpec := range fdw.Options {
		curOptSpec, exists := info.Options[name]

		switch {
		case desiredOptSpec.Ensure == apiv1.EnsurePresent && !exists:
			clauses = append(clauses, fmt.Sprintf("ADD %s '%s'",
				pgx.Identifier{name}.Sanitize(), desiredOptSpec.Value))

		case desiredOptSpec.Ensure == apiv1.EnsurePresent && exists:
			if desiredOptSpec.Value != curOptSpec.Value {
				clauses = append(clauses, fmt.Sprintf("SET %s '%s'",
					pgx.Identifier{name}.Sanitize(), desiredOptSpec.Value))
			}

		case desiredOptSpec.Ensure == apiv1.EnsureAbsent && exists:
			clauses = append(clauses, fmt.Sprintf("DROP %s", pgx.Identifier{name}.Sanitize()))
		}
	}

	if len(clauses) == 0 {
		return nil
	}

	// Build SQL
	changeOptionSQL := fmt.Sprintf(
		"ALTER FOREIGN DATA WRAPPER %s OPTIONS (%s)", pgx.Identifier{fdw.Name}.Sanitize(),
		strings.Join(clauses, ", "),
	)

	if _, err := db.ExecContext(ctx, changeOptionSQL); err != nil {
		return fmt.Errorf("altering options of foreign data wrapper %w", err)
	}
	contextLogger.Info("altered foreign data wrapper options", "name", fdw.Name, "options", fdw.Options)

	return nil
}

func updateDatabaseFDW(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec, info *fdwInfo) error {
	contextLogger := log.FromContext(ctx)

	// Alter Handler
	if fdw.Handler != nil {
		handler := *fdw.Handler
		switch {
		case handler == "" && info.Handler != "-":
			changeHandlerSQL := fmt.Sprintf("ALTER FOREIGN DATA WRAPPER %s NO HANDLER",
				pgx.Identifier{fdw.Name}.Sanitize(),
			)

			if _, err := db.ExecContext(ctx, changeHandlerSQL); err != nil {
				return fmt.Errorf("removing handler of foreign data wrapper %w", err)
			}

			contextLogger.Info("removed foreign data wrapper handler", "name", fdw.Name)

		case handler != "" && info.Handler != handler:
			changeHandlerSQL := fmt.Sprintf("ALTER FOREIGN DATA WRAPPER %s HANDLER %s",
				pgx.Identifier{fdw.Name}.Sanitize(),
				pgx.Identifier{*fdw.Handler}.Sanitize(),
			)

			if _, err := db.ExecContext(ctx, changeHandlerSQL); err != nil {
				return fmt.Errorf("altering handler of foreign data wrapper %w", err)
			}

			contextLogger.Info("altered foreign data wrapper handler", "name", fdw.Name, "handler", *fdw.Handler)
		}
	}

	// Alter Validator
	if fdw.Validator != nil {
		validator := *fdw.Validator
		switch {
		case validator == "" && info.Validator != "-":
			changeValidatorSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s NO VALIDATOR",
				pgx.Identifier{fdw.Name}.Sanitize(),
			)

			if _, err := db.ExecContext(ctx, changeValidatorSQL); err != nil {
				return fmt.Errorf("removing validator of foreign data wrapper %w", err)
			}

			contextLogger.Info("removed foreign data wrapper validator", "name", fdw.Name)
		case validator != "" && info.Validator != validator:
			changeValidatorSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s VALIDATOR %s",
				pgx.Identifier{fdw.Name}.Sanitize(),
				pgx.Identifier{*fdw.Validator}.Sanitize(),
			)

			if _, err := db.ExecContext(ctx, changeValidatorSQL); err != nil {
				return fmt.Errorf("altering validator of foreign data wrapper %w", err)
			}

			contextLogger.Info("altered foreign data wrapper validator", "name", fdw.Name, "validator", *fdw.Validator)
		}
	}

	// Alter the owner
	if len(fdw.Owner) > 0 && fdw.Owner != info.Owner {
		changeOwnerSQL := fmt.Sprintf(
			"ALTER FOREIGN DATA WRAPPER %s OWNER TO %v",
			pgx.Identifier{fdw.Name}.Sanitize(),
			pgx.Identifier{fdw.Owner}.Sanitize(),
		)

		if _, err := db.ExecContext(ctx, changeOwnerSQL); err != nil {
			return fmt.Errorf("altering owner of foreign data wrapper %w", err)
		}

		contextLogger.Info("altered foreign data wrapper owner", "name", fdw.Name, "owner", fdw.Owner)
	}

	// Alter Options
	if err := updateFDWOptions(ctx, db, fdw, info); err != nil {
		return err
	}

	return nil
}

func dropDatabaseFDW(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec) error {
	contextLogger := log.FromContext(ctx)
	query := fmt.Sprintf("DROP FOREIGN DATA WRAPPER IF EXISTS %s", pgx.Identifier{fdw.Name}.Sanitize())
	_, err := db.ExecContext(
		ctx,
		query)
	if err != nil {
		contextLogger.Error(err, "while dropping foreign data wrapper", "query", query)
		return err
	}
	contextLogger.Info("dropped foreign data wrapper", "name", fdw.Name)
	return nil
}
