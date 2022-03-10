/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package run implements the "instance run" subcommand of the operator
package run

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/run/lifecycle"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver/metricserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

var scheme = runtime.NewScheme()

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
}

// NewCmd creates the "instance run" subcommand
func NewCmd() *cobra.Command {
	var pgData string
	var podName string
	var clusterName string
	var namespace string

	cmd := &cobra.Command{
		Use: "run [flags]",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := log.IntoContext(cmd.Context(), log.GetLogger())
			instance := postgres.NewInstance()

			instance.PgData = pgData
			instance.Namespace = namespace
			instance.PodName = podName
			instance.ClusterName = clusterName

			return runSubCommand(ctx, instance)
		},
	}

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be started up")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	return cmd
}

func runSubCommand(ctx context.Context, instance *postgres.Instance) error {
	var err error
	setupLog := log.WithName("setup")

	setupLog.Info("Starting Cloud Native PostgreSQL Instance Manager",
		"version", versions.Version,
		"build", versions.Info)

	mgr, err := ctrl.NewManager(config.GetConfigOrDie(), ctrl.Options{
		Scheme:    scheme,
		Namespace: instance.Namespace,
		NewCache: cache.BuilderWithOptions(cache.Options{
			SelectorsByObject: cache.SelectorsByObject{
				&apiv1.Cluster{}: {
					Field: fields.OneTermEqualSelector("metadata.name", instance.ClusterName),
				},
			},
		}),
		// We don't need a cache for secrets and configmap, as all reloads
		// should be driven by changes in the Cluster we are watching
		ClientDisableCacheFor: []client.Object{
			&corev1.Secret{},
			&corev1.ConfigMap{},
		},
		MetricsBindAddress: "0", // TODO: merge metrics to the manager one
	})
	if err != nil {
		setupLog.Error(err, "unable to set up overall controller manager")
		return err
	}

	metricsServer, err := metricserver.New(instance)
	if err != nil {
		return err
	}

	reconciler := controller.NewInstanceReconciler(instance, mgr.GetClient(), metricsServer)
	err = ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Complete(reconciler)
	if err != nil {
		setupLog.Error(err, "unable to create controller")
		return err
	}

	postgresLifecycleManager := lifecycle.NewPostgres(ctx, instance, reconciler.GetInitialized())
	if err = mgr.Add(postgresLifecycleManager); err != nil {
		setupLog.Error(err, "unable to create instance runnable")
		return err
	}

	// TODO move to separate runnable
	if err = logpipe.Start(); err != nil {
		log.Error(err, "Error while starting the logging collector routine")
		return err
	}

	if err = mgr.Add(metricsServer); err != nil {
		setupLog.Error(err, "unable to add metrics webserver runnable")
		return err
	}

	remoteSrv, err := webserver.NewRemoteWebServer(instance)
	if err != nil {
		return err
	}
	if err = mgr.Add(remoteSrv); err != nil {
		setupLog.Error(err, "unable to add remote webserver runnable")
		return err
	}

	localSrv, err := webserver.NewLocalWebServer(instance)
	if err != nil {
		return err
	}
	if err = mgr.Add(localSrv); err != nil {
		setupLog.Error(err, "unable to add local webserver runnable")
		return err
	}

	setupLog.Info("starting controller-runtime manager")
	if err := mgr.Start(postgresLifecycleManager.GetContext()); err != nil {
		setupLog.Error(err, "unable to run controller-runtime manager")
		return err
	}

	return nil
}
