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

package logs

import (
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

func clusterCmd() *cobra.Command {
	cl := ClusterLogs{}

	cmd := &cobra.Command{
		Use:   "cluster <ClusterName>",
		Short: "Logs for cluster's pods",
		Long:  "Collects the logs for all pods in a cluster into a single stream or OutputFile",
		Args:  plugin.RequiresArguments(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cl.ClusterName = args[0]
			cl.Namespace = plugin.Namespace
			cl.Ctx = cmd.Context()
			cl.Client = plugin.ClientInterface
			if cl.Follow {
				return FollowCluster(cl)
			}
			return saveClusterLogs(cl)
		},
	}

	cmd.Flags().StringVarP(&cl.OutputFile, "output", "o", "",
		"Output OutputFile")
	cmd.Flags().BoolVarP(&cl.timestamp, "timestamps", "t", false,
		"Prepend human-readable timestamp to each log line. If set, logs start from current time")
	cmd.Flags().Int64Var(&cl.TailLines, "tail", -1,
		"Number of lines from the end of the logs to show for each pod. By default there is no limit")
	cmd.Flags().BoolVarP(&cl.Follow, "Follow", "f", false,
		"Follow cluster logs (watches for new and re-created pods)")

	return cmd
}
