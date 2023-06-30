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
	"time"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

func clusterCmd() *cobra.Command {
	var (
		file                 string
		logTimeStamp, follow bool
	)

	cmd := &cobra.Command{
		Use:   "cluster <clusterName>",
		Short: "Logs for cluster's pods",
		Long:  "Collects the logs for all pods in a cluster into a single stream or file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			now := time.Now().UTC()
			if follow {
				return followCluster(cmd.Context(), clusterName, plugin.Namespace,
					logTimeStamp, now)
			}
			return saveClusterLogs(cmd.Context(), clusterName, plugin.Namespace,
				logTimeStamp, file)
		},
	}

	cmd.Flags().StringVarP(&file, "File", "F", "",
		"Output file")
	cmd.Flags().BoolVarP(&logTimeStamp, "timestamps", "t", false,
		"Prepend human-readable timestamp to each log line")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false,
		"Follow cluster logs (watches for new and re-created pods)")

	return cmd
}
