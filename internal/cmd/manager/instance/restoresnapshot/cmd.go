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

// Package restoresnapshot implements the "instance restoresnapshot" subcommand of the operator
package restoresnapshot

import (
	"context"
	"encoding/base64"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// NewCmd creates the "restoresnapshot" subcommand
func NewCmd() *cobra.Command {
	var (
		clusterName   string
		namespace     string
		pgData        string
		pgWal         string
		backupLabel   string
		tablespaceMap string
		immediate     bool
	)

	cmd := &cobra.Command{
		Use:           "restoresnapshot [flags]",
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			info := postgres.InitInfo{
				ClusterName: clusterName,
				Namespace:   namespace,
				PgData:      pgData,
				PgWal:       pgWal,
			}

			if backupLabel != "" {
				res, err := base64.StdEncoding.DecodeString(backupLabel)
				if err != nil {
					return err
				}
				info.BackupLabelFile = res
			}

			if tablespaceMap != "" {
				res, err := base64.StdEncoding.DecodeString(tablespaceMap)
				if err != nil {
					return err
				}
				info.TablespaceMapFile = res
			}

			err := execute(ctx, info, immediate)
			if err != nil {
				log.Error(err, "Error while recovering Volume Snapshot backup")
			}
			return err
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"cluster containing the PVC snapshot to be restored")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be restored")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "The PGWAL to be restored")
	cmd.Flags().StringVar(&backupLabel, "backuplabel", "", "The restore backup_label file content")
	cmd.Flags().StringVar(&tablespaceMap, "tablespacemap", "", "The restore tablespace_map file content")
	cmd.Flags().BoolVar(&immediate, "immediate", false, "Do not start PostgreSQL but just recover the snapshot")

	return cmd
}

func execute(ctx context.Context, info postgres.InitInfo, immediate bool) error {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return err
	}

	return info.RestoreSnapshot(ctx, typedClient, immediate)
}
