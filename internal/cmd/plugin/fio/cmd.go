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

package fio

import (
	"context"

	"github.com/spf13/cobra"
)

// NewCmd initializes the fio command
func NewCmd() *cobra.Command {
	var storageClassName, deploymentName, pvcSize string
	var dryRun bool

	fioCmd := &cobra.Command{
		Use:     "fio [name]",
		Short:   "Creates a fio deployment,pvc and configmap.",
		Args:    validateCommandArgs,
		Long:    `Creates a fio deployment that will execute a fio job on the specified pvc.`,
		Example: jobExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			pgBenchArgs := args[1:]
			deploymentName = args[0]
			fioCommand := newFioCommand(deploymentName, storageClassName, pvcSize, dryRun, pgBenchArgs)
			return fioCommand.execute(ctx)
		},
	}
	fioCmd.Flags().StringVar(
		&storageClassName,
		"storageClass",
		"",
		"The name of the storageClass that will be used by pvc.",
	)
	fioCmd.Flags().StringVar(
		&pvcSize,
		"pvcSize",
		"2Gi",
		"The size of the pvc which will be used to benchmark.",
	)
	fioCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"When true prints the deployment manifest instead of creating it",
	)

	return fioCmd
}

func validateCommandArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
		return err
	}
	return nil
}
