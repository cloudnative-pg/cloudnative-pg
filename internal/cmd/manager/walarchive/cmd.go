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

// Package walarchive implement the wal-archive command
package walarchive

import (
	"errors"
	"fmt"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/archiver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/local"
)

// errSwitchoverInProgress is raised when there is a switchover in progress
// and the new primary have not completed the promotion
var errSwitchoverInProgress = fmt.Errorf("switchover in progress, refusing archiving")

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	var podName string
	var pgData string
	cmd := cobra.Command{
		Use:           "wal-archive [name]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			const logErrorMessage = "failed to run wal-archive command"

			contextLog := log.WithName("wal-archive")
			ctx := log.IntoContext(cobraCmd.Context(), contextLog)

			if podName == "" {
				err := fmt.Errorf("no pod-name value passed and failed to extract it from POD_NAME env variable")
				contextLog.Error(err, logErrorMessage)
				return err
			}

			localClient := local.NewClient()
			cluster, errCluster := localClient.Cache().GetCluster()
			if errCluster != nil {
				return fmt.Errorf("failed to get cluster: %w", errCluster)
			}

			if err := archiver.Run(ctx, podName, pgData, cluster, args[0]); err != nil {
				if errors.Is(err, errSwitchoverInProgress) {
					contextLog.Warning("Refusing to archive WALs until the switchover is not completed",
						"err", err)
				} else {
					contextLog.Error(err, logErrorMessage)
				}
				if reqErr := localClient.Cluster().SetWALArchiveStatusCondition(ctx, err.Error()); reqErr != nil {
					contextLog.Error(reqErr, "while invoking the set wal archive condition endpoint")
				}
				return err
			}

			if err := localClient.Cluster().SetWALArchiveStatusCondition(ctx, ""); err != nil {
				contextLog.Error(err, "while invoking the set wal archive condition endpoint")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of the "+
		"current pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be used")

	return &cmd
}
