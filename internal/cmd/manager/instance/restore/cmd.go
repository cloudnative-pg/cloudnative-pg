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

// Package restore implements the "instance restore" subcommand of the operator
package restore

import (
	"context"
	"errors"
	"os"

	barmanCommand "github.com/cloudnative-pg/barman-cloud/pkg/command"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
)

// NewCmd creates the "restore" subcommand
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var pgData string
	var pgWal string
	var cli client.Client

	cmd := &cobra.Command{
		Use:           "restore [flags]",
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			contextLogger := log.FromContext(ctx)
			mgr, err := buildLocalWebserverMgr(ctx, clusterName, namespace)
			if err != nil {
				contextLogger.Error(err, "while building the manager")
				return err
			}

			go func() {
				if err := mgr.Start(ctx); err != nil {
					contextLogger.Error(err, "unable to start local webserver manager")
				}
			}()

			cli = mgr.GetClient()

			// we will wait this way for the mgr and informers to be online
			return management.WaitForGetClusterWithClient(cmd.Context(), cli, client.ObjectKey{
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

			return restoreSubCommand(ctx, info, cli)
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
		"the cluster and the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be restored")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "The PGWAL to be restored")

	return cmd
}

func buildLocalWebserverMgr(ctx context.Context, clusterName string, namespace string) (manager.Manager, error) {
	contextLogger := log.FromContext(ctx)
	runtimeScheme := scheme.BuildWithAllKnownScheme()
	mgr, err := controllerruntime.NewManager(controllerruntime.GetConfigOrDie(), controllerruntime.Options{
		Scheme: runtimeScheme,
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
	})
	if err != nil {
		return nil, err
	}

	localSrv, err := webserver.NewLocalWebServer(
		postgres.NewInstance().WithClusterName(clusterName).WithNamespace(namespace),
		mgr.GetClient(),
		mgr.GetEventRecorderFor("local-webserver"),
	)
	if err != nil {
		return nil, err
	}
	if err = mgr.Add(localSrv); err != nil {
		contextLogger.Error(err, "unable to add local webserver runnable")
		return nil, err
	}

	return mgr, nil
}

func restoreSubCommand(ctx context.Context, info postgres.InitInfo, cli client.Client) error {
	contextLogger := log.FromContext(ctx)
	err := info.CheckTargetDataDirectory(ctx)
	if err != nil {
		return err
	}

	err = info.Restore(ctx, cli)
	if err != nil {
		contextLogger.Error(err, "Error while restoring a backup")
		cleanupDataDirectoryIfNeeded(ctx, err, info.PgData)
		return err
	}

	contextLogger.Info("restore command execution completed without errors")

	return nil
}

func cleanupDataDirectoryIfNeeded(ctx context.Context, restoreError error, dataDirectory string) {
	contextLogger := log.FromContext(ctx)

	var barmanError *barmanCommand.CloudRestoreError
	if !errors.As(restoreError, &barmanError) {
		return
	}

	if !barmanError.IsRetriable() {
		return
	}

	contextLogger.Info("Cleaning up data directory", "directory", dataDirectory)
	if err := fileutils.RemoveDirectory(dataDirectory); err != nil && !os.IsNotExist(err) {
		contextLogger.Error(
			err,
			"error occurred cleaning up data directory",
			"directory", dataDirectory)
	}
}
