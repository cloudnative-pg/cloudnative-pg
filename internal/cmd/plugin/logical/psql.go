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

package logical

import (
	"context"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/psql"
)

// RunSQL execs a SQL statement while connected via `psql` to
// to a Pod of a cluster, targeting a passed connection string
func RunSQL(
	ctx context.Context,
	clusterName string,
	connectionString string,
	sqlCommand string,
) error {
	cmd, err := getSQLCommand(ctx, clusterName, connectionString, sqlCommand, "-qAt")
	if err != nil {
		return err
	}

	return cmd.Run()
}

// RunSQLWithOutput execs a SQL statement while connected via `psql` to
// to a Pod of a cluster, targeting a passed connection string
func RunSQLWithOutput(
	ctx context.Context,
	clusterName string,
	connectionString string,
	sqlCommand string,
) ([]byte, error) {
	cmd, err := getSQLCommand(ctx, clusterName, connectionString, sqlCommand, "-qAt")
	if err != nil {
		return nil, err
	}

	return cmd.Output()
}

func getSQLCommand(
	ctx context.Context,
	clusterName string,
	connectionString string,
	sqlCommand string,
	args ...string,
) (*psql.Command, error) {
	psqlArgs := make([]string, 0, 5+len(args))
	psqlArgs = append(psqlArgs,
		connectionString,
		"-U",
		"postgres",
		"-c",
		sqlCommand,
	)
	psqlArgs = append(psqlArgs, args...)
	psqlOptions := psql.CommandOptions{
		Replica:     false,
		Namespace:   plugin.Namespace,
		AllocateTTY: false,
		PassStdin:   false,
		Args:        psqlArgs,
		Name:        clusterName,
	}

	return psql.NewCommand(ctx, psqlOptions)
}
