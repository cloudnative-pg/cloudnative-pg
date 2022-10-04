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

package hibernate

import (
	"github.com/spf13/cobra"
)

var (
	hibernateOnCmd = &cobra.Command{
		Use:   "on [cluster]",
		Short: "Hibernates the cluster named [cluster]",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}

			hibernateOn, err := newOnCommand(cmd.Context(), clusterName, force)
			if err != nil {
				return err
			}

			return hibernateOn.execute()
		},
	}

	hibernateOffCmd = &cobra.Command{
		Use:   "off [cluster]",
		Short: "Bring the cluster named [cluster] back from hibernation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			return hibernateOff(cmd.Context(), clusterName)
		},
	}
)

// NewCmd initializes the hibernate command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hibernate",
		Short: `Hibernation related commands`,
	}

	cmd.AddCommand(hibernateOnCmd)
	cmd.AddCommand(hibernateOffCmd)

	hibernateOnCmd.Flags().Bool(
		"force",
		false,
		"Force the hibernation procedure even if the preconditions are not met")

	return cmd
}
