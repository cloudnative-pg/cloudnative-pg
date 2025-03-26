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

// Package pgbouncer implements the "pgbouncer" subcommand of the operator
package pgbouncer

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/pgbouncer/run"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// NewCmd creates the "instance" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "pgbouncer",
		Short:         "pgbouncer management subfeatures",
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return os.MkdirAll(postgres.TemporaryDirectory, 0o1777) //nolint:gosec
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("missing subcommand")
		},
	}

	cmd.AddCommand(run.NewCmd())

	return cmd
}
