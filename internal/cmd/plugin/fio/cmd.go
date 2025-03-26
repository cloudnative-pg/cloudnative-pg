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

package fio

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// NewCmd initializes the fio command
func NewCmd() *cobra.Command {
	var storageClassName, deploymentName, pvcSize string
	var dryRun bool

	fioCmd := &cobra.Command{
		Use:     "fio [name]",
		Short:   "Creates a fio deployment, pvc and configmap",
		Args:    cobra.MinimumNArgs(1),
		Long:    `Creates a fio deployment that will execute a fio job on the specified pvc.`,
		Example: jobExample,
		GroupID: plugin.GroupIDMiscellaneous,
		RunE: func(_ *cobra.Command, args []string) error {
			ctx := context.Background()
			fioArgs := args[1:]
			deploymentName = args[0]
			fioCommand := newFioCommand(deploymentName, storageClassName, pvcSize, dryRun, fioArgs)
			return fioCommand.execute(ctx)
		},
		PreRun: func(_ *cobra.Command, _ []string) {
			if !dryRun {
				fmt.Println("Running this directly to the cluster may produce a disruption in the service, " +
					"are you sure you want to proceed? (y/n)")
				var input string
				_, err := fmt.Scanln(&input)
				if err != nil {
					os.Exit(1)
				}
				if input != "y" {
					os.Exit(0)
				}
			}
		},
		PostRun: func(_ *cobra.Command, _ []string) {
			if !dryRun {
				fmt.Printf("To remove this test you need to delete the Deployment, ConfigMap "+
					"and PVC with the name %v\n\nThe most simple way to do this is to re-run the command that was run"+
					"to generate the deployment with the --dry-run flag and pipe that output to kubectl delete, e.g.:\n\n"+
					"kubectl cnpg fio <fio-job-name> --dry-run | kubectl delete -f -\n", deploymentName)
			}
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
