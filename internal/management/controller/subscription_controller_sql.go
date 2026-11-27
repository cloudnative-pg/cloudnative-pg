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
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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
	version, err := r.getPostgresMajorVersion()
	if err != nil {
		return fmt.Errorf("while getting the PostgreSQL major version: %w", err)
	}

	currentParameters, err := getSubscriptionEnabledOnlyParameters(
		ctx, db, obj.Spec.Name, version, obj.Spec.Parameters)
	if err != nil {
		return fmt.Errorf("while getting the current subscription parameters: %w", err)
	}

	sqls := toSubscriptionAlterSQL(obj, connString, version, currentParameters)
	for _, sqlQuery := range sqls {
		if _, err := db.ExecContext(ctx, sqlQuery); err != nil {
			return err
		}
	}

	return nil
}

// subscriptionParametersRequiringDisable lists the subscription parameters that
// PostgreSQL refuses to change while the subscription is enabled.
//
// See https://www.postgresql.org/docs/current/sql-altersubscription.html#SQL-ALTERSUBSCRIPTION-PARAMS-SET
var subscriptionParametersRequiringDisable = []string{"failover", "two_phase"}

// requestsEnabledOnlyParameter reports whether the spec asks for any of the
// parameters that PostgreSQL only accepts on a disabled subscription.
func requestsEnabledOnlyParameter(parameters map[string]string) bool {
	for _, key := range subscriptionParametersRequiringDisable {
		if _, present := parameters[key]; present {
			return true
		}
	}

	return false
}

// getSubscriptionEnabledOnlyParameters reads, from the catalog, the current value
// of the parameters that cannot be altered while the subscription is enabled.
//
// Only the parameters that the running PostgreSQL version actually exposes are
// read: `subfailover` was added in PostgreSQL 17, and `two_phase` only became
// alterable in PostgreSQL 18.
//
// A parameter missing from the returned map means "unknown", and the caller
// leaves it in the ALTER statement rather than guessing.
func getSubscriptionEnabledOnlyParameters(
	ctx context.Context,
	db *sql.DB,
	name string,
	pgMajorVersion int,
	requestedParameters map[string]string,
) (map[string]string, error) {
	// `subfailover` only exists from PostgreSQL 17 on, and a subscription that asks
	// for none of these parameters cannot be blocked by them. In both cases the
	// catalog lookup would be a round-trip with nothing to decide.
	if pgMajorVersion < 17 || !requestsEnabledOnlyParameter(requestedParameters) {
		return nil, nil
	}

	columns := []string{"subfailover"}
	if pgMajorVersion >= 18 {
		// 'd' means disabled, 'p' pending and 'e' enabled: anything but 'd' is on
		columns = append(columns, "subtwophasestate <> 'd'")
	}

	row := db.QueryRowContext(
		ctx,
		fmt.Sprintf(
			`SELECT %s FROM pg_catalog.pg_subscription WHERE subname = $1`,
			strings.Join(columns, ", "),
		),
		name)

	values := make([]bool, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The subscription disappeared between the existence check and this
			// query. Returning no values lets the ALTER go through unchanged and
			// surface the real error.
			return nil, nil
		}
		return nil, err
	}

	result := make(map[string]string, len(values))
	for i, parameter := range subscriptionParametersRequiringDisable[:len(values)] {
		result[parameter] = strconv.FormatBool(values[i])
	}

	return result, nil
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

// toSubscriptionAlterSQL builds the statements that align an existing subscription
// with its spec.
//
// currentParameters holds the catalog value of the parameters that PostgreSQL only
// accepts on a disabled subscription; a parameter already matching it is left out
// of the SET, because re-sending it would be rejected even though nothing changed.
func toSubscriptionAlterSQL(
	obj *apiv1.Subscription,
	connString string,
	pgMajorVersion int,
	currentParameters map[string]string,
) []string {
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
		parameters := dropUnchangedEnabledOnlyParameters(
			filterSubscriptionUpdatableParameters(obj.Spec.Parameters, pgMajorVersion),
			currentParameters,
		)
		if len(parameters) > 0 {
			result = append(result,
				fmt.Sprintf(
					"ALTER SUBSCRIPTION %s SET (%s)",
					pgx.Identifier{obj.Spec.Name}.Sanitize(),
					toPostgresParameters(parameters),
				),
			)
		}
	}

	return result
}

// dropUnchangedEnabledOnlyParameters removes the parameters that cannot be altered
// on an enabled subscription and whose requested value already matches the catalog.
//
// Without this, a spec change that does not touch `failover` still re-sends it, and
// PostgreSQL rejects the whole statement with
//
//	ERROR: cannot set option "failover" for enabled subscription
//
// which leaves the subscription unable to accept any further change.
//
// A value that genuinely differs is kept: changing it does require disabling the
// subscription first, and that error belongs to the user, not to a parameter they
// never asked to modify.
func dropUnchangedEnabledOnlyParameters(parameters, currentParameters map[string]string) map[string]string {
	if len(currentParameters) == 0 {
		return parameters
	}

	result := make(map[string]string, len(parameters))
	for key, value := range parameters {
		if current, known := currentParameters[key]; known && samePostgresBoolean(value, current) {
			continue
		}
		result[key] = value
	}

	return result
}

// samePostgresBoolean compares two PostgreSQL boolean literals. Both `on` and `true`
// mean the same to PostgreSQL, so comparing the raw strings would re-send a parameter
// that did not change. An unparsable value is treated as different, so the statement
// goes through and PostgreSQL reports what is wrong with it.
func samePostgresBoolean(a, b string) bool {
	parsedA, err := postgres.ParsePostgresConfigBoolean(a)
	if err != nil {
		return false
	}
	parsedB, err := postgres.ParsePostgresConfigBoolean(b)
	if err != nil {
		return false
	}

	return parsedA == parsedB
}

func filterSubscriptionUpdatableParameters(parameters map[string]string, pgMajorVersion int) map[string]string {
	// Only a limited set of the parameters can be updated
	// see https://www.postgresql.org/docs/current/sql-altersubscription.html#SQL-ALTERSUBSCRIPTION-PARAMS-SET
	allowedParameters := []string{
		"slot_name",
		"synchronous_commit",
		"binary",
		"streaming",
		"disable_on_error",
		"password_required",
		"run_as_owner",
		"origin",
		"failover",
	}
	if pgMajorVersion >= 18 {
		allowedParameters = append(allowedParameters, "two_phase")
	}
	filteredParameters := make(map[string]string, len(parameters))
	for _, key := range allowedParameters {
		if _, present := parameters[key]; present {
			filteredParameters[key] = parameters[key]
		}
	}
	return filteredParameters
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
