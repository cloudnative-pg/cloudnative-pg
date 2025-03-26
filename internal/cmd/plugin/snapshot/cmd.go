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

package snapshot

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd implements the `snapshot` subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "snapshot CLUSTER",
		Short:   "DEPRECATED (use `backup -m volumeSnapshot` instead)",
		Long:    "Replaced by `kubectl cnpg backup <cluster-name> -m volumeSnapshot`",
		GroupID: plugin.GroupIDDatabase,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("This command was replaced by `kubectl cnpg backup <cluster-name> -m volumeSnapshot`")
			fmt.Println("IMPORTANT: if you are using VolumeSnapshots on 1.20, you should upgrade to the latest minor release")
			return errors.New("command removed")
		},
	}

	return cmd
}
