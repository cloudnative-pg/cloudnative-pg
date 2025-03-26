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

package destroy

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd create the new "destroy" subcommand
func NewCmd() *cobra.Command {
	destroyCmd := &cobra.Command{
		Use:     "destroy CLUSTER INSTANCE",
		Short:   "Destroy the instance named CLUSTER-INSTANCE with the associated PVC",
		GroupID: plugin.GroupIDCluster,
		Args:    plugin.RequiresArguments(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}

			keepPVC, _ := cmd.Flags().GetBool("keep-pvc")
			return Destroy(ctx, clusterName, node, keepPVC)
		},
	}

	destroyCmd.Flags().BoolP("keep-pvc", "k", false,
		"Keep the PVC but detach it from instance")

	return destroyCmd
}
