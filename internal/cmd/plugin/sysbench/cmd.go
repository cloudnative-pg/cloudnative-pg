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

package sysbench

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

const defaultSysbenchImage = "perconalab/sysbench:1.1"

// NewCmd initializes the sysbench command
func NewCmd() *cobra.Command {
	run := &sysbenchRun{}

	sysBenchCmd := &cobra.Command{
		Use:     "sysbench <cluster-name> [-- sysbench_command_args...]",
		Short:   "Creates a sysbench job",
		Args:    plugin.RequiresArguments(1),
		Long:    "Creates a sysbench job to run against the specified Postgres Cluster.",
		GroupID: plugin.GroupIDMiscellaneous,
		Example: jobExample,

		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() > 1 {
				return fmt.Errorf("sysbench_command_args should be passed after the -- delimiter")
			}
			run.clusterName = args[0]
			run.sysbenchCommandArgs = args[1:]

			return run.execute(cmd.Context())
		},
	}

	sysBenchCmd.Flags().StringVar(
		&run.jobName,
		"job-name",
		"",
		"Name of the job, defaulting to: cluster-name-sysbench-xxxx",
	)

	sysBenchCmd.Flags().StringVar(
		&run.dbName,
		"db-name",
		"app",
		"The name of the database that will be used by sysbench. Defaults to: app",
	)
	sysBenchCmd.Flags().StringVar(
		&run.sysbenchImage,
		"sysbench-image",
		defaultSysbenchImage,
		"The sysbench image to use for the job.",
	)

	sysBenchCmd.Flags().BoolVar(
		&run.dryRun,
		"dry-run",
		false,
		"Whether to print the job manifest without creating the job. Defaults to false.",
	)

	sysBenchCmd.Flags().StringSliceVar(
		&run.nodeSelector,
		"node-selector",
		[]string{},
		"Node label selector, in the format: key=value,key2=value",
	)

	sysBenchCmd.Flags().Int32Var(
		&run.ttlSecondsAfterFinished,
		"ttl",
		0,
		"TTL seconds to clean up the job after it finishes. Defaults to no TTL.",
	)

	return sysBenchCmd
}
