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

package destroy

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd create the new "promote" subcommand
func NewCmd() *cobra.Command {
	promoteCmd := &cobra.Command{
		Use:   "destroy [cluster] [instance_id]",
		Short: "Destroy the instance named [cluster]-[node] and the associated PVC",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			instanceID := args[1]
			keepPVC, _ := cmd.Flags().GetBool("keep-pvc")
			return Destroy(ctx, clusterName, instanceID, keepPVC)
		},
	}

	promoteCmd.Flags().BoolP("keep-pvc", "k", false,
		"Keep the PVC but detach it from instance")

	return promoteCmd
}
