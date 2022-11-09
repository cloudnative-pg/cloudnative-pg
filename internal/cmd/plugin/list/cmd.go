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

package list

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd create the new "status" subcommand
func NewCmd() *cobra.Command {
	var allNamespaces bool

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all created PostgreSQL cluster",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			output, _ := cmd.Flags().GetString("output")

			return List(ctx, allNamespaces, plugin.OutputFormat(output))
		},
	}

	listCmd.PersistentFlags().BoolVarP(&allNamespaces,
		"all-namespaces", "A", false, "Apply operation to all clusters in all namespaces")
	listCmd.Flags().StringP(
		"output", "o", "text", "Output format. One of text|json")

	return listCmd
}
