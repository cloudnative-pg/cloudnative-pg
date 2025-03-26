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

package psql

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd creates the "psql" command
func NewCmd() *cobra.Command {
	var replica bool
	var allocateTTY bool
	var passStdin bool

	cmd := &cobra.Command{
		Use:   "psql CLUSTER [-- PSQL_ARGS...]",
		Short: "Start a psql session targeting a CloudNativePG cluster",
		Args:  validatePsqlArgs,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		Long:    "This command will start an interactive psql session inside a PostgreSQL Pod created by CloudNativePG.",
		GroupID: plugin.GroupIDMiscellaneous,
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			psqlArgs := args[1:]
			psqlOptions := CommandOptions{
				Replica:     replica,
				Namespace:   plugin.Namespace,
				Context:     plugin.KubeContext,
				AllocateTTY: allocateTTY,
				PassStdin:   passStdin,
				Args:        psqlArgs,
				Name:        clusterName,
			}

			psqlCommand, err := NewCommand(cmd.Context(), psqlOptions)
			if err != nil {
				return err
			}

			return psqlCommand.Exec()
		},
	}

	cmd.Flags().BoolVar(
		&replica,
		"replica",
		false,
		"Connects to the first replica on the pod list (by default connects to the primary)",
	)

	cmd.Flags().BoolVarP(
		&allocateTTY,
		"tty",
		"t",
		true,
		"Whether to allocate a TTY for the psql process",
	)

	cmd.Flags().BoolVarP(
		&passStdin,
		"stdin",
		"i",
		true,
		"Whether to pass stdin to the container",
	)

	return cmd
}

func validatePsqlArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
		return err
	}

	if cmd.ArgsLenAtDash() > 1 {
		return fmt.Errorf("psqlArgs should be passed after the -- delimiter")
	}

	return nil
}
