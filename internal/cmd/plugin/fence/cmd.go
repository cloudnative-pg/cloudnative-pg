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

package fence

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

var (
	fenceOnCmd = &cobra.Command{
		Use:   "on CLUSTER INSTANCE",
		Short: `Fence an instance named CLUSTER-INSTANCE`,
		Args:  plugin.RequiresArguments(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}

			return fencingOn(cmd.Context(), clusterName, node)
		},
	}

	fenceOffCmd = &cobra.Command{
		Use:   "off CLUSTER INSTANCE",
		Short: `Remove fence for an instance named CLUSTER-INSTANCE`,
		Args:  plugin.RequiresArguments(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}
			return fencingOff(cmd.Context(), clusterName, node)
		},
	}
)

// NewCmd creates the new "fencing" command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "fencing",
		Short:   `Fencing related commands`,
		GroupID: plugin.GroupIDCluster,
	}
	cmd.AddCommand(fenceOnCmd)
	cmd.AddCommand(fenceOffCmd)

	return cmd
}
