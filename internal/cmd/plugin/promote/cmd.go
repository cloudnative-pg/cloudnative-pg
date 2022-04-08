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

package promote

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// NewCmd create the new "promote" subcommand
func NewCmd() *cobra.Command {
	promoteCmd := &cobra.Command{
		Use:   "promote [cluster] [node]",
		Short: "Promote the pod named [cluster]-[node] or [node] to primary",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]
			node := args[1]
			if _, err := strconv.Atoi(args[1]); err == nil {
				node = fmt.Sprintf("%s-%s", clusterName, node)
			}
			return Promote(ctx, clusterName, node)
		},
	}

	return promoteCmd
}
