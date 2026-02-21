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
	Name      string            `json:"name"`
	Handler   string            `json:"handler"`
	Validator string            `json:"validator"`
	Owner     string            `json:"owner"`
	Options   map[string]string `json:"options"`
}

type serverInfo struct {
	Name    string            `json:"name"`
	FDWName string            `json:"fdwName"`
	Options map[string]string `json:"options"`
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
	fmt.Fprintf(&sqlCreateExtension, "CREATE EXTENSION %s ", pgx.Identifier{ext.Name}.Sanitize())
	if len(ext.Version) > 0 {
		fmt.Fprintf(&sqlCreateExtension, " VERSION %s", pgx.Identifier{ext.Version}.Sanitize())
	}
	if len(ext.Schema) > 0 {
		fmt.Fprintf(&sqlCreateExtension, " SCHEMA %s", pgx.Identifier{ext.Schema}.Sanitize())
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
	fmt.Fprintf(&sqlCreateExtension, "CREATE SCHEMA %s ", pgx.Identifier{schema.Name}.Sanitize())
	if len(schema.Owner) > 0 {
		fmt.Fprintf(&sqlCreateExtension, " AUTHORIZATION %s", pgx.Identifier{schema.Owner}.Sanitize())
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

// extractOptionsClauses takes a list of apiv1.OptionSpec and returns the present options as clauses
// suitable to be joined with ", " inside a CREATE ... OPTIONS (...) statement.
func extractOptionsClauses(options []apiv1.OptionSpec) []string {
	opts := make([]string, 0, len(options))
	for _, optionSpec := range options {
		if optionSpec.Ensure == apiv1.EnsureAbsent {
			continue
		}
		opts = append(opts, fmt.Sprintf("%s %s", pgx.Identifier{optionSpec.Name}.Sanitize(),
			pq.QuoteLiteral(optionSpec.Value)))
	}

	return opts
}

// calculateAlterOptionsClauses returns the list of option alteration clauses (ADD / SET / DROP)
// needed to reconcile the currentOptions of a database object with the desiredOptions.
//
// The returned slice is suitable to be joined with ", " inside an ALTER ... OPTIONS (...)
// statement. Order of emitted clauses follows the order of desiredOptions, enabling
// predictable application.
func calculateAlterOptionsClauses(desiredOptions []apiv1.OptionSpec, currentOptions map[string]string) []string {
	var clauses []string
	for _, desiredOptSpec := range desiredOptions {
		curOptValue, exists := currentOptions[desiredOptSpec.Name]

		switch {
		case desiredOptSpec.Ensure == apiv1.EnsurePresent && !exists:
			clauses = append(clauses, fmt.Sprintf("ADD %s %s",
				pgx.Identifier{desiredOptSpec.Name}.Sanitize(), pq.QuoteLiteral(desiredOptSpec.Value)))

		case desiredOptSpec.Ensure == apiv1.EnsurePresent && exists && desiredOptSpec.Value != curOptValue:
			clauses = append(clauses, fmt.Sprintf("SET %s %s",
				pgx.Identifier{desiredOptSpec.Name}.Sanitize(), pq.QuoteLiteral(desiredOptSpec.Value)))

		case desiredOptSpec.Ensure == apiv1.EnsureAbsent && exists:
			clauses = append(clauses, fmt.Sprintf("DROP %s",
				pgx.Identifier{desiredOptSpec.Name}.Sanitize()))
		}
	}

	return clauses
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
	var (
		result     fdwInfo
		optionsRaw pq.StringArray
	)

	if err := db.QueryRowContext(
		ctx, detectDatabaseFDWSQL,
		fdw.Name).
		Scan(&result.Name, &result.Handler, &result.Validator, &optionsRaw, &result.Owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("while scanning if FDW %q exists: %w", fdw.Name, err)
	}

	// Extract options from SQL raw format(e.g. -{host=localhost,port=5432}) to type OptSpec
	opts, err := parseOptions(optionsRaw)
	if err != nil {
		return nil, fmt.Errorf("while parsing options of foreign data wrapper %q: %w", fdw.Name, err)
	}
	result.Options = opts

	return &result, nil
}

// updateDatabaseFDWUsage updates the usage permissions for a foreign data wrapper
// based on the provided FDW specification.
func updateDatabaseFDWUsage(ctx context.Context, db *sql.DB, fdw *apiv1.FDWSpec) error {
	const objectTypeForeignDataWrapper = "FOREIGN DATA WRAPPER"
	return applyUsagePermissions(ctx, db, objectTypeForeignDataWrapper, fdw.Name, fdw.Usages)
}

// updateDatabaseForeignServerUsage updates the usage permissions of a foreign server in the database.
// It supports granting or revoking usage permissions for specified users.
func updateDatabaseForeignServerUsage(ctx context.Context, db *sql.DB, server *apiv1.ServerSpec) error {
	const objectTypeForeignServer = "FOREIGN SERVER"
	return applyUsagePermissions(ctx, db, objectTypeForeignServer, server.Name, server.Usages)
}

// applyUsagePermissions is a generic helper to grant or revoke USAGE permissions
// for FOREIGN DATA WRAPPER / FOREIGN SERVER objects, avoiding duplicated logic.
func applyUsagePermissions(
	ctx context.Context,
	db *sql.DB,
	objectType string,
	objectName string,
	usages []apiv1.UsageSpec,
) error {
	contextLogger := log.FromContext(ctx)

	if len(usages) == 0 {
		return nil
	}

	for _, usageSpec := range usages {
		sanitizedObject := pgx.Identifier{objectName}.Sanitize()
		sanitizedUser := pgx.Identifier{usageSpec.Name}.Sanitize()

		switch usageSpec.Type {
		case apiv1.GrantUsageSpecType:
			mutation := fmt.Sprintf("GRANT USAGE ON %s %s TO %s", objectType, sanitizedObject, sanitizedUser)
			if _, err := db.ExecContext(ctx, mutation); err != nil {
				return fmt.Errorf("granting usage of %s: %w", objectType, err)
			}
			contextLogger.Info("granted usage", "type", objectType, "name", objectName, "user", usageSpec.Name)

		case apiv1.RevokeUsageSpecType:
			mutation := fmt.Sprintf("REVOKE USAGE ON %s %s FROM %s", objectType, sanitizedObject, sanitizedUser) // nolint:gosec
			if _, err := db.ExecContext(ctx, mutation); err != nil {
				return fmt.Errorf("revoking usage of %s: %w", objectType, err)
			}
			contextLogger.Info("revoked usage", "type", objectType, "name", objectName, "user", usageSpec.Name)

		default:
			contextLogger.Warning(
				"unknown usage type", "type", usageSpec.Type, "objectType", objectType, "name", objectName)
		}
	}

	return nil
}

func createDatabaseFDW(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateFDW strings.Builder
	fmt.Fprintf(&sqlCreateFDW, "CREATE FOREIGN DATA WRAPPER %s ", pgx.Identifier{fdw.Name}.Sanitize())

	// Create Handler
	if len(fdw.Handler) > 0 {
		switch fdw.Handler {
		case "-":
			sqlCreateFDW.WriteString("NO HANDLER ")
		default:
			fmt.Fprintf(&sqlCreateFDW, "HANDLER %s ", pgx.Identifier{fdw.Handler}.Sanitize())
		}
	}

	// Create Validator
	if len(fdw.Validator) > 0 {
		switch fdw.Validator {
		case "-":
			sqlCreateFDW.WriteString("NO VALIDATOR ")
		default:
			fmt.Fprintf(&sqlCreateFDW, "VALIDATOR %s ", pgx.Identifier{fdw.Validator}.Sanitize())
		}
	}

	if opts := extractOptionsClauses(fdw.Options); len(opts) > 0 {
		sqlCreateFDW.WriteString("OPTIONS (" + strings.Join(opts, ", ") + ")")
	}

	_, err := db.ExecContext(ctx, sqlCreateFDW.String())
	if err != nil {
		contextLogger.Error(err, "while creating foreign data wrapper", "query", sqlCreateFDW.String())
		return err
	}
	contextLogger.Info("created foreign data wrapper", "name", fdw.Name)

	// Update usage permissions
	if len(fdw.Usages) > 0 {
		if err := updateDatabaseFDWUsage(ctx, db, &fdw); err != nil {
			return err
		}
	}

	return nil
}

func updateDatabaseFDW(ctx context.Context, db *sql.DB, fdw apiv1.FDWSpec, info *fdwInfo) error {
	contextLogger := log.FromContext(ctx)

	// Alter Handler
	if len(fdw.Handler) > 0 && fdw.Handler != info.Handler {
		switch fdw.Handler {
		case "-":
			changeHandlerSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s NO HANDLER",
				pgx.Identifier{fdw.Name}.Sanitize(),
			)
			if _, err := db.ExecContext(ctx, changeHandlerSQL); err != nil {
				return fmt.Errorf("removing handler of foreign data wrapper %w", err)
			}
			contextLogger.Info("removed foreign data wrapper handler", "name", fdw.Name)

		default:
			changeHandlerSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s HANDLER %s",
				pgx.Identifier{fdw.Name}.Sanitize(),
				pgx.Identifier{fdw.Handler}.Sanitize(),
			)
			if _, err := db.ExecContext(ctx, changeHandlerSQL); err != nil {
				return fmt.Errorf("altering handler of foreign data wrapper %w", err)
			}
			contextLogger.Info("altered foreign data wrapper handler", "name", fdw.Name, "handler", fdw.Handler)
		}
	}

	// Alter Validator
	if len(fdw.Validator) > 0 && fdw.Validator != info.Validator {
		switch fdw.Validator {
		case "-":
			changeValidatorSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s NO VALIDATOR",
				pgx.Identifier{fdw.Name}.Sanitize(),
			)

			if _, err := db.ExecContext(ctx, changeValidatorSQL); err != nil {
				return fmt.Errorf("removing validator of foreign data wrapper %w", err)
			}

			contextLogger.Info("removed foreign data wrapper validator", "name", fdw.Name)

		default:
			changeValidatorSQL := fmt.Sprintf(
				"ALTER FOREIGN DATA WRAPPER %s VALIDATOR %s",
				pgx.Identifier{fdw.Name}.Sanitize(),
				pgx.Identifier{fdw.Validator}.Sanitize(),
			)
			if _, err := db.ExecContext(ctx, changeValidatorSQL); err != nil {
				return fmt.Errorf("altering validator of foreign data wrapper %w", err)
			}

			contextLogger.Info("altered foreign data wrapper validator", "name", fdw.Name, "validator", fdw.Validator)
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

	if toUpdateOpts := calculateAlterOptionsClauses(fdw.Options, info.Options); len(toUpdateOpts) > 0 {
		changeOptionSQL := fmt.Sprintf(
			"ALTER FOREIGN DATA WRAPPER %s OPTIONS (%s)", pgx.Identifier{fdw.Name}.Sanitize(),
			strings.Join(toUpdateOpts, ", "),
		)

		if _, err := db.ExecContext(ctx, changeOptionSQL); err != nil {
			return fmt.Errorf("altering options of foreign data wrapper %w", err)
		}
		contextLogger.Info("altered foreign data wrapper options", "name", fdw.Name, "options", fdw.Options)
	}

	// Update usage permissions
	if len(fdw.Usages) > 0 {
		if err := updateDatabaseFDWUsage(ctx, db, &fdw); err != nil {
			return err
		}
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

const detectDatabaseForeignServerSQL = `
SELECT
	srvname, fdwname, srvoptions
FROM pg_foreign_server fs
JOIN pg_foreign_data_wrapper fdw ON fs.srvfdw = fdw.oid
WHERE srvname = $1
`

func getDatabaseForeignServerInfo(ctx context.Context, db *sql.DB, server apiv1.ServerSpec) (*serverInfo, error) {
	var (
		result     serverInfo
		optionsRaw pq.StringArray
	)

	if err := db.QueryRowContext(
		ctx, detectDatabaseForeignServerSQL,
		server.Name).Scan(&result.Name, &result.FDWName, &optionsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("while scanning if foreign server %q exists: %w", server.Name, err)
	}

	opts, err := parseOptions(optionsRaw)
	if err != nil {
		return nil, fmt.Errorf("while parsing options of foreign server %q: %w", server.Name, err)
	}
	result.Options = opts

	return &result, nil
}

// createDatabaseForeignServer creates a foreign server in the database.
func createDatabaseForeignServer(ctx context.Context, db *sql.DB, server apiv1.ServerSpec) error {
	contextLogger := log.FromContext(ctx)

	var sqlCreateServer strings.Builder
	fmt.Fprintf(&sqlCreateServer, "CREATE SERVER %s FOREIGN DATA WRAPPER %s ",
		pgx.Identifier{server.Name}.Sanitize(),
		pgx.Identifier{server.FdwName}.Sanitize())

	if opts := extractOptionsClauses(server.Options); len(opts) > 0 {
		sqlCreateServer.WriteString("OPTIONS (" + strings.Join(opts, ", ") + ")")
	}

	_, err := db.ExecContext(ctx, sqlCreateServer.String())
	if err != nil {
		contextLogger.Error(err, "while creating foreign server", "query", sqlCreateServer.String())
		return err
	}
	contextLogger.Info("created foreign server", "name", server.Name)

	if len(server.Usages) > 0 {
		if err := updateDatabaseForeignServerUsage(ctx, db, &server); err != nil {
			return err
		}
	}

	return nil
}

// updateDatabaseForeignServer updates the configuration of a foreign server in the database.
func updateDatabaseForeignServer(ctx context.Context, db *sql.DB, server apiv1.ServerSpec, info *serverInfo) error {
	contextLogger := log.FromContext(ctx)

	// Alter Options
	if toUpdateOpts := calculateAlterOptionsClauses(server.Options, info.Options); len(toUpdateOpts) > 0 {
		changeOptionSQL := fmt.Sprintf(
			"ALTER SERVER %s OPTIONS (%s)", pgx.Identifier{server.Name}.Sanitize(),
			strings.Join(toUpdateOpts, ", "),
		)

		if _, err := db.ExecContext(ctx, changeOptionSQL); err != nil {
			return fmt.Errorf("altering options of foreign server %w", err)
		}
		contextLogger.Info("altered foreign server options", "name", server.Name, "options", server.Options)
	}

	if len(server.Usages) > 0 {
		if err := updateDatabaseForeignServerUsage(ctx, db, &server); err != nil {
			return err
		}
	}

	return nil
}

// dropDatabaseForeignServer drops a foreign server from the database.
func dropDatabaseForeignServer(ctx context.Context, db *sql.DB, server apiv1.ServerSpec) error {
	contextLogger := log.FromContext(ctx)
	query := fmt.Sprintf("DROP SERVER IF EXISTS %s", pgx.Identifier{server.Name}.Sanitize())
	_, err := db.ExecContext(
		ctx,
		query)
	if err != nil {
		contextLogger.Error(err, "while dropping foreign server", "query", query)
		return err
	}
	contextLogger.Info("dropped foreign server", "name", server.Name)
	return nil
}
