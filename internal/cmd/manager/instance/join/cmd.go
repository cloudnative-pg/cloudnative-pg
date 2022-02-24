/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package join implements the "instance join" subcommand of the operator
package join

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// NewCmd creates the new "join" command
func NewCmd() *cobra.Command {
	var pgData string
	var parentNode string
	var podName string
	var clusterName string
	var namespace string

	cmd := &cobra.Command{
		Use: "join [options]",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			instance := postgres.NewInstance()

			// The following are needed to correctly
			// download the secret containing the TLS
			// certificates
			instance.Namespace = namespace
			instance.PodName = podName
			instance.ClusterName = clusterName

			info := postgres.InitInfo{
				PgData:     pgData,
				ParentNode: parentNode,
				PodName:    podName,
			}

			return joinSubCommand(ctx, instance, info)
		},
	}

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
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
	err := info.VerifyPGData()
	if err != nil {
		return err
	}

	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		log.Error(err, "Error creating Kubernetes client")
		return err
	}

	// Let's download the crypto material from the cluster
	// secrets.
	reconciler := controller.NewInstanceReconciler(instance, client)
	if err != nil {
		log.Error(err, "Error creating reconciler to download certificates")
		return err
	}

	var cluster apiv1.Cluster
	err = reconciler.GetClient().Get(ctx,
		ctrl.ObjectKey{Namespace: instance.Namespace, Name: instance.ClusterName},
		&cluster)
	if err != nil {
		log.Error(err, "Error while getting cluster")
		return err
	}

	reconciler.RefreshSecrets(ctx, &cluster)

	err = info.Join()
	if err != nil {
		log.Error(err, "Error joining node")
		return err
	}

	return nil
}
