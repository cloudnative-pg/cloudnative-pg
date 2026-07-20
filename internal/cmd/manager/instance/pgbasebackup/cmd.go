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

// Package pgbasebackup implement the pgbasebackup bootstrap method
package pgbasebackup

import (
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/bootstrap"
)

// NewCmd creates the "pgbasebackup" subcommand
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var pgData string
	var pgWal string

	cmd := &cobra.Command{
		Use: "pgbasebackup",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			info := postgres.InitInfo{
				ClusterName: clusterName,
				Namespace:   namespace,
				PgData:      pgData,
				PgWal:       pgWal,
			}

			return bootstrap.Execute(ctx, client, nil, info, bootstrap.Instruction{Mode: bootstrap.ModePgBaseBackup})
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "the PGWAL to be created")

	return cmd
}
