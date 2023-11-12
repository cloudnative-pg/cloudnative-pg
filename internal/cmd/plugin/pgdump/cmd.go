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

package pgdump

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd creates the "pgdump" command
func NewCmd() *cobra.Command {
	var replica bool
	var allocateTTY bool
	var passStdin bool

	cmd := &cobra.Command{
		Use:   "pgdump [cluster] [-- pgdumpArgs...]",
		Short: "Start a pgdump comment targeting a CloudNativePG cluster",
		Args:  validatePgdumpArgs,
		Long:  "This command will start an pgdump command inside a PostgreSQL Pod created by CloudNativePG.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			pgdumpArgs := args[1:]
			pgdumpOptions := pgdumpCommandOptions{
				replica:     replica,
				namespace:   plugin.Namespace,
				passStdin:   passStdin,
				args:        pgdumpArgs,
				name:        clusterName,
			}

			pgdumpCommand, err := newPgdumpCommand(cmd.Context(), pgdumpOptions)
			if err != nil {
				return err
			}

			return pgdumpCommand.exec()
		},
	}

	cmd.Flags().BoolVar(
		&replica,
		"replica",
		false,
		"Connects to the first replica on the pod list (by default connects to the primary)",
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

func validatePgdumpArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
		return err
	}

	if cmd.ArgsLenAtDash() > 1 {
		return fmt.Errorf("pgdumpArgs should be passed after -- delimitator")
	}

	return nil
}
