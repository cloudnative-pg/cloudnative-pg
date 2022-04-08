/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package restart

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// NewCmd creates the new "reset" command
func NewCmd() *cobra.Command {
	restartCmd := &cobra.Command{
		Use:   "restart clusterName [instance]",
		Short: `Restart a cluster or a single instance in a cluster`,
		Long: `If only the cluster name is specified, the whole cluster will be restarted, 
rolling out new configurations if present.
If a specific instance is specified, only that instance will be restarted, 
in-place if it is a primary, deleting the pod if it is a replica.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clusterName := args[0]
			if len(args) == 1 {
				return restart(ctx, clusterName)
			}
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}
			return instanceRestart(ctx, clusterName, node)
		},
	}

	return restartCmd
}
