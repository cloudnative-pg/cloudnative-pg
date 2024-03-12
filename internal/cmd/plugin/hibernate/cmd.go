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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

var (
	hibernateOnCmd = &cobra.Command{
		Use:   "on [cluster]",
		Short: "Hibernates the cluster named [cluster]",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
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
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			off := newOffCommand(cmd.Context(), clusterName)
			return off.execute()
		},
	}

	hibernateStatusCmd = &cobra.Command{
		Use:   "status [cluster]",
		Short: "Prints the hibernation status for the [cluster]",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			rawOutput, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}

			outputFormat := plugin.OutputFormat(rawOutput)
			switch outputFormat {
			case plugin.OutputFormatJSON, plugin.OutputFormatYAML:
				return newStatusCommandStructuredOutput(cmd.Context(), clusterName, outputFormat).execute()
			case plugin.OutputFormatText:
				return newStatusCommandTextOutput(cmd.Context(), clusterName).execute()
			default:
				return fmt.Errorf("output: %s is not supported by the hibernate CLI", rawOutput)
			}
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
	cmd.AddCommand(hibernateStatusCmd)

	hibernateOnCmd.Flags().Bool(
		"force",
		false,
		"Force the hibernation procedure even if the preconditions are not met")
	hibernateStatusCmd.Flags().
		StringP(
			"output",
			"o",
			"text",
			"Output format. One of text, json, or yaml",
		)

	return cmd
}
