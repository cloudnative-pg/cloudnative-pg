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

package pgbench

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd initializes the pgBench command
func NewCmd() *cobra.Command {
	pgBenchCmd := &cobra.Command{
		Use:     "pgbench [cluster] [pgBenchCommandArgs...]",
		Short:   "create pgbench job",
		Long:    `This command will create a pgbench job with given values : cluster and pgBenchCommandArgs.`,
		Example: jobExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			jobOptions, err := initJobOptions(cmd, args)
			if err != nil {
				return err
			}
			return Create(ctx, jobOptions)
		},
	}
	pgBenchCmd.Flags().String(
		"pgbench-job-name", "", "default value : <clusterName>-pgbench-XXXXX")
	pgBenchCmd.Flags().String(
		"db-name", "app", "default value : app")
	pgBenchCmd.Flags().Bool(
		"dry-run", false, "If specified, the job is not created and it will print YAML content")
	return pgBenchCmd
}
