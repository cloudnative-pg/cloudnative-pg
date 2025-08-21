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

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

func (r *PublicationReconciler) alignPublication(ctx context.Context, obj *apiv1.Publication) error {
	db, err := r.getDB(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_catalog.pg_publication
	        WHERE pubname = $1
		`,
		obj.Spec.Name)
	var count int
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("while getting publication status (scan): %w", err)
	}

	if count > 0 {
		if err := r.patchPublication(ctx, db, obj); err != nil {
			return fmt.Errorf("while patching publication: %w", err)
		}
		return nil
	}

	if err := r.createPublication(ctx, db, obj); err != nil {
		return fmt.Errorf("while creating publication: %w", err)
	}

	return nil
}

func (r *PublicationReconciler) patchPublication(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Publication,
) error {
	sqls := toPublicationAlterSQL(obj)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

func (r *PublicationReconciler) createPublication(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Publication,
) error {
	sqlQuery := toPublicationCreateSQL(obj)
	_, err := db.ExecContext(ctx, sqlQuery)
	return err
}

func toPublicationCreateSQL(obj *apiv1.Publication) string {
	createQuery := fmt.Sprintf(
		"CREATE PUBLICATION %s %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		toPublicationTargetSQL(&obj.Spec.Target),
	)
	if len(obj.Spec.Parameters) > 0 {
		createQuery = fmt.Sprintf("%s WITH (%s)", createQuery, toPostgresParameters(obj.Spec.Parameters))
	}

	return createQuery
}

func toPublicationAlterSQL(obj *apiv1.Publication) []string {
	result := make([]string, 0, 2)

	if len(obj.Spec.Target.Objects) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s SET %s",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				toPublicationTargetObjectsSQL(&obj.Spec.Target),
			),
		)
	}

	if len(obj.Spec.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER PUBLICATION %s SET (%s)",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				toPostgresParameters(obj.Spec.Parameters),
			),
		)
	}

	return result
}

func executeDropPublication(ctx context.Context, db *sql.DB, name string) error {
	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pgx.Identifier{name}.Sanitize()),
	); err != nil {
		return fmt.Errorf("while dropping publication: %w", err)
	}

	return nil
}

func toPublicationTargetSQL(obj *apiv1.PublicationTarget) string {
	if obj.AllTables {
		return "FOR ALL TABLES"
	}

	result := toPublicationTargetObjectsSQL(obj)
	if len(result) > 0 {
		result = fmt.Sprintf("FOR %s", result)
	}
	return result
}

func toPublicationTargetObjectsSQL(obj *apiv1.PublicationTarget) string {
	result := ""
	for _, object := range obj.Objects {
		if len(result) > 0 {
			result += ", "
		}
		result += toPublicationObjectSQL(&object)
	}

	return result
}

func toPublicationObjectSQL(obj *apiv1.PublicationTargetObject) string {
	if len(obj.TablesInSchema) > 0 {
		return fmt.Sprintf("TABLES IN SCHEMA %s", pgx.Identifier{obj.TablesInSchema}.Sanitize())
	}

	result := strings.Builder{}
	result.WriteString("TABLE ")

	if obj.Table.Only {
		result.WriteString("ONLY ")
	}

	if len(obj.Table.Schema) > 0 {
		result.WriteString(fmt.Sprintf("%s.", pgx.Identifier{obj.Table.Schema}.Sanitize()))
	}

	result.WriteString(pgx.Identifier{obj.Table.Name}.Sanitize())

	if len(obj.Table.Columns) > 0 {
		sanitizedColumns := make([]string, 0, len(obj.Table.Columns))
		for _, column := range obj.Table.Columns {
			sanitizedColumns = append(sanitizedColumns, pgx.Identifier{column}.Sanitize())
		}
		result.WriteString(fmt.Sprintf(" (%s)", strings.Join(sanitizedColumns, ", ")))
	}

	return result.String()
}
