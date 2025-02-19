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

package controller

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

func (r *SubscriptionReconciler) alignSubscription(
	ctx context.Context,
	obj *apiv1.Subscription,
	connString string,
) error {
	db, err := r.getDB(obj.Spec.DBName)
	if err != nil {
		return fmt.Errorf("while getting DB connection: %w", err)
	}

	row := db.QueryRowContext(
		ctx,
		`
		SELECT count(*)
		FROM pg_catalog.pg_subscription
	    WHERE subname = $1
		`,
		obj.Spec.Name)
	if row.Err() != nil {
		return fmt.Errorf("while getting subscription status: %w", row.Err())
	}

	var count int
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("while getting subscription status (scan): %w", err)
	}

	if count > 0 {
		if err := r.patchSubscription(ctx, db, obj, connString); err != nil {
			return fmt.Errorf("while patching subscription: %w", err)
		}
		return nil
	}

	if err := r.createSubscription(ctx, db, obj, connString); err != nil {
		return fmt.Errorf("while creating subscription: %w", err)
	}

	return nil
}

func (r *SubscriptionReconciler) patchSubscription(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Subscription,
	connString string,
) error {
	sqls := toSubscriptionAlterSQL(obj, connString)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

func (r *SubscriptionReconciler) createSubscription(
	ctx context.Context,
	db *sql.DB,
	obj *apiv1.Subscription,
	connString string,
) error {
	sqlQuery := toSubscriptionCreateSQL(obj, connString)
	_, err := db.ExecContext(ctx, sqlQuery)
	return err
}

func toSubscriptionCreateSQL(obj *apiv1.Subscription, connString string) string {
	createQuery := fmt.Sprintf(
		"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pq.QuoteLiteral(connString),
		pgx.Identifier{obj.Spec.PublicationName}.Sanitize(),
	)
	if len(obj.Spec.Parameters) > 0 {
		createQuery = fmt.Sprintf("%s WITH (%s)", createQuery, toPostgresParameters(obj.Spec.Parameters))
	}

	return createQuery
}

func toSubscriptionAlterSQL(obj *apiv1.Subscription, connString string) []string {
	result := make([]string, 0, 3)

	setPublicationSQL := fmt.Sprintf(
		"ALTER SUBSCRIPTION %s SET PUBLICATION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pgx.Identifier{obj.Spec.PublicationName}.Sanitize(),
	)

	setConnStringSQL := fmt.Sprintf(
		"ALTER SUBSCRIPTION %s CONNECTION %s",
		pgx.Identifier{obj.Spec.Name}.Sanitize(),
		pq.QuoteLiteral(connString),
	)
	result = append(result, setPublicationSQL, setConnStringSQL)

	if len(obj.Spec.Parameters) > 0 {
		result = append(result,
			fmt.Sprintf(
				"ALTER SUBSCRIPTION %s SET (%s)",
				pgx.Identifier{obj.Spec.Name}.Sanitize(),
				toPostgresParameters(obj.Spec.Parameters),
			),
		)
	}

	return result
}

func executeDropSubscription(ctx context.Context, db *sql.DB, name string) error {
	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{name}.Sanitize()),
	); err != nil {
		return fmt.Errorf("while dropping subscription: %w", err)
	}

	return nil
}
