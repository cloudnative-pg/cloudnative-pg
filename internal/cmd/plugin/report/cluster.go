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

package report

import (
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

func clusterCmd() *cobra.Command {
	var (
		file, output string
		includeLogs  bool
	)

	cmd := &cobra.Command{
		Use:   "cluster [clusterName]",
		Short: "Report cluster resources, pods, events, logs (opt-in)",
		Long:  "Collects combined information on the cluster in a Zip file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			return Cluster(cmd.Context(), clusterName, plugin.Namespace,
				plugin.OutputFormat(output), file, includeLogs)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", reportName("cluster")+".zip",
		"Output file (timestamp computed at each run)")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml",
		"Output format (yaml or json)")
	cmd.Flags().BoolVarP(&includeLogs, "logs", "l", false, "include logs")

	return cmd
}
