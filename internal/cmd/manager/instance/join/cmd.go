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

// Package join implements the "instance join" subcommand of the operator
package join

import (
	"context"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
)

// NewCmd creates the new "join" command
func NewCmd() *cobra.Command {
	var pgData string
	var pgWal string
	var parentNode string
	var podName string
	var clusterName string
	var namespace string

	cmd := &cobra.Command{
		Use: "join [options]",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// The fields in the instance are needed to correctly
			// download the secret containing the TLS
			// certificates
			instance := postgres.NewInstance().
				WithNamespace(namespace).
				WithPodName(podName).
				WithClusterName(clusterName)

			info := postgres.InitInfo{
				PgData:     pgData,
				PgWal:      pgWal,
				ParentNode: parentNode,
				PodName:    podName,
			}

			return joinSubCommand(ctx, instance, info)
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "the PGWAL to be created")
	cmd.Flags().StringVar(&parentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of "+
		"the current cluster in k8s, used to download TLS certificates")

	return cmd
}

func joinSubCommand(ctx context.Context, instance *postgres.Instance, info postgres.InitInfo) error {
	if err := info.CheckTargetDataDirectory(ctx); err != nil {
		return err
	}

	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		log.Error(err, "Error creating Kubernetes client")
		return err
	}

	// Create a fake reconciler just to download the secrets and
	// the cluster definition
	metricExporter := metricserver.NewExporter(instance)
	reconciler := controller.NewInstanceReconciler(instance, client, metricExporter)

	// Download the cluster definition from the API server
	var cluster apiv1.Cluster
	if err := reconciler.GetClient().Get(ctx,
		ctrl.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
		&cluster,
	); err != nil {
		log.Error(err, "Error while getting cluster")
		return err
	}

	// Since we're directly using the reconciler here, we cannot
	// tell if the secrets were correctly downloaded or not.
	// If they were the following "pg_basebackup" command will work, if
	// they don't "pg_basebackup" with fail, complaining that the
	// cryptographic material is not available.
	// So it doesn't make a real difference.
	//
	// Besides this, we should improve this situation to have
	// a real error handling.
	reconciler.RefreshSecrets(ctx, &cluster)

	// Run "pg_basebackup" to download the data directory from the primary
	if err := info.Join(&cluster); err != nil {
		log.Error(err, "Error joining node")
		return err
	}

	return nil
}
