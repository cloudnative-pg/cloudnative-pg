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

// Package instance implements the "instance" subcommand of the operator
package instance

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/initdb"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/join"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/logicalsnapshot"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/pgbasebackup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/restore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/run"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/status"
)

// NewCmd creates the "instance" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Instance management subfeatures",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("missing subcommand")
		},
	}

	cmd.AddCommand(initdb.NewCmd())
	cmd.AddCommand(join.NewCmd())
	cmd.AddCommand(run.NewCmd())
	cmd.AddCommand(status.NewCmd())
	cmd.AddCommand(pgbasebackup.NewCmd())
	cmd.AddCommand(restore.NewCmd())
	cmd.AddCommand(logicalsnapshot.NewCmd())

	return cmd
}
