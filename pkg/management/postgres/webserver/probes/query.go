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

package probes

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// PgQueryChecker checks if the PostgreSQL server can execute a simple query
type PgQueryChecker struct {
	MaximumLag *int64
}

// Evaluate implements the Checker interface
func (c PgQueryChecker) Evaluate(ctx context.Context, instance *postgres.Instance) error {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("while getting superuser connection pool: %w", err)
	}

	if err := superUserDB.PingContext(ctx); err != nil {
		return fmt.Errorf("while pinging database: %w", err)
	}

	return nil
}
