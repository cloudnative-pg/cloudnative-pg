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

package reload

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd creates the new "reset" command
func NewCmd() *cobra.Command {
	restartCmd := &cobra.Command{
		Use:   "reload [clusterName]",
		Short: `Reload the cluster`,
		Long:  `Triggers a reconciliation loop for all the cluster's instances, rolling out new configurations if present.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			return Reload(ctx, clusterName)
		},
	}

	return restartCmd
}
