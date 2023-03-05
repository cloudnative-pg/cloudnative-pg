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

package psql

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// NewCmd creates the "psql" command
func NewCmd() *cobra.Command {
	var role string
	var allocateTTY bool
	var passStdin bool

	cmd := &cobra.Command{
		Use:   "psql [cluster] [-- psqlArgs...]",
		Short: "Start a psql session targeting a CloudNativePG cluster",
		Args:  validatePsqlArgs,
		Long:  "This command will start an interactive psql session inside a PostgreSQL Pod created by CloudNativePG.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			psqlArgs := args[1:]

			if role != specs.ClusterRoleLabelPrimary && role != specs.ClusterRoleLabelReplica {
				return fmt.Errorf("invalid pod role")
			}

			psqlOptions := psqlCommandOptions{
				role:        role,
				namespace:   plugin.Namespace,
				allocateTTY: allocateTTY,
				passStdin:   passStdin,
				args:        psqlArgs,
				name:        clusterName,
			}

			psqlCommand, err := newPsqlCommand(cmd.Context(), psqlOptions)
			if err != nil {
				return err
			}

			return psqlCommand.exec()
		},
	}

	cmd.Flags().StringVarP(
		&role,
		"role",
		"r",
		"primary",
		"The role of the Pod to connect to. Valid values are 'primary' and 'replica'",
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
		return fmt.Errorf("psqlArgs should be passed after -- delimitator")
	}

	return nil
}
