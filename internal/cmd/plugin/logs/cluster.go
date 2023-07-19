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
	cl := clusterLogs{}

	cmd := &cobra.Command{
		Use:   "cluster <clusterName>",
		Short: "Logs for cluster's pods",
		Long:  "Collects the logs for all pods in a cluster into a single stream or outputFile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl.clusterName = args[0]
			cl.namespace = plugin.Namespace
			cl.ctx = cmd.Context()
			cl.client = plugin.ClientInterface
			if cl.follow {
				return followCluster(cl)
			}
			return saveClusterLogs(cl)
		},
	}

	cmd.Flags().StringVarP(&cl.outputFile, "output", "o", "",
		"Output outputFile")
	cmd.Flags().BoolVarP(&cl.timestamp, "timestamps", "t", false,
		"Prepend human-readable timestamp to each log line, if provided, logs since current timestamp will display")
	cmd.Flags().Int64Var(&cl.tailLines, "tail", -1,
		" Lines of log file from each pod to retrieve, defaults to show all log lines")
	cmd.Flags().BoolVarP(&cl.follow, "follow", "f", false,
		"Follow cluster logs (watches for new and re-created pods)")

	return cmd
}
