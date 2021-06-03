/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package join implements the "instance join" subcommand of the operator
package join

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
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
			var instance postgres.Instance

			// The following are needed to correctly
			// download the secret containing the TLS
			// certificates
			instance.Namespace = namespace
			instance.PodName = podName
			instance.ClusterName = clusterName

			info := postgres.JoinInfo{
				PgData:     pgData,
				ParentNode: parentNode,
				PodName:    podName,
			}

			return joinSubCommand(ctx, &instance, info)
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

func joinSubCommand(ctx context.Context, instance *postgres.Instance, info postgres.JoinInfo) error {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Log.Error(err, "Error while checking for an existent PGData")
		return err
	}
	if status {
		log.Log.Info("PGData already exists, no need to init")
		return fmt.Errorf("pgdata already existent")
	}

	// Let's download the crypto material from the cluster
	// secrets.
	reconciler, err := controller.NewInstanceReconciler(instance)
	if err != nil {
		log.Log.Error(err, "Error creating reconciler to download certificates")
		return err
	}

	var cluster apiv1.Cluster
	err = reconciler.GetClient().Get(ctx,
		ctrl.ObjectKey{Namespace: instance.Namespace, Name: instance.ClusterName},
		&cluster)
	if err != nil {
		log.Log.Error(err, "Error while getting cluster")
		return err
	}

	_, err = reconciler.RefreshReplicationUserCertificate(ctx, &cluster)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS server certificates")
		return err
	}

	_, err = reconciler.RefreshClientCA(ctx, &cluster)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS Client CA certificates")
		return err
	}

	_, err = reconciler.RefreshServerCA(ctx, &cluster)
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS Server CA certificates")
		return err
	}

	err = info.Join()
	if err != nil {
		log.Log.Error(err, "Error joining node")
		return err
	}

	return nil
}
