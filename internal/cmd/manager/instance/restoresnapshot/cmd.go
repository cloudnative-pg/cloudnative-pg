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

package restoresnapshot

import (
	"context"
	"encoding/base64"
	"errors"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			contextLogger := log.FromContext(cmd.Context())

			// Canceling this context
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Step 1: build the manager
			mgr, err := buildManager(clusterName, namespace)
			if err != nil {
				contextLogger.Error(err, "while building the manager")
				return err
			}

			// Step 1.1: add the local webserver to the manager
			localSrv, err := webserver.NewLocalWebServer(
				postgres.NewInstance().WithClusterName(clusterName).WithNamespace(namespace),
				mgr.GetClient(),
				mgr.GetEventRecorderFor("local-webserver"), //nolint:staticcheck
			)
			if err != nil {
				return err
			}
			if err = mgr.Add(localSrv); err != nil {
				contextLogger.Error(err, "unable to add local webserver runnable")
				return err
			}

			// Step 2: add the restore process to the manager
			restoreProcess := restoreRunnable{
				cli:         mgr.GetClient(),
				clusterName: clusterName,
				namespace:   namespace,
				pgData:      pgData,
				pgWal:       pgWal,
				immediate:   immediate,
				cancel:      cancel,
			}
			if mgr.Add(&restoreProcess) != nil {
				contextLogger.Error(err, "while building the restore process")
				return err
			}

			if backupLabel != "" {
				res, err := base64.StdEncoding.DecodeString(backupLabel)
				if err != nil {
					return err
				}
				restoreProcess.backupLabelFile = res
			}

			if tablespaceMap != "" {
				res, err := base64.StdEncoding.DecodeString(tablespaceMap)
				if err != nil {
					return err
				}
				restoreProcess.tablespaceMapFile = res
			}

			// Step 3: start everything
			if err := mgr.Start(ctx); err != nil {
				contextLogger.Error(err, "restore error")
				return err
			}

			if !errors.Is(ctx.Err(), context.Canceled) {
				contextLogger.Error(err, "error while recovering backup")
				return err
			}

			return nil
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

func buildManager(clusterName string, namespace string) (manager.Manager, error) {
	return controllerruntime.NewManager(controllerruntime.GetConfigOrDie(), controllerruntime.Options{
		Scheme: scheme.BuildWithAllKnownScheme(),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&apiv1.Cluster{}: {
					Field: fields.OneTermEqualSelector("metadata.name", clusterName),
					Namespaces: map[string]cache.Config{
						namespace: {},
					},
				},
			},
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.Secret{},
					&corev1.ConfigMap{},
					// todo(armru): we should remove the backup endpoints from the local webserver
					&apiv1.Backup{},
				},
			},
		},
		LeaderElection: false,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
}
