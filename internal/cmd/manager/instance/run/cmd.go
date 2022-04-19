/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package run implements the "instance run" subcommand of the operator
package run

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/instance/run/lifecycle"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/concurrency"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/logpipe"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver/metricserver"
	pg "github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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

			return retry.OnError(retry.DefaultRetry, isRunSubCommandRetryable, func() error {
				return runSubCommand(ctx, instance)
			})
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

	postgresStartConditions := concurrency.MultipleExecuted{}
	exitedConditions := concurrency.MultipleExecuted{}

	reconciler := controller.NewInstanceReconciler(instance, mgr.GetClient(), metricsServer)
	err = ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Complete(reconciler)
	if err != nil {
		setupLog.Error(err, "unable to create controller")
		return err
	}
	postgresStartConditions = append(postgresStartConditions, reconciler.GetExecutedCondition())

	// postgres CSV logs handler (PGAudit too)
	postgresLogPipe := logpipe.NewLogPipe()
	if err := mgr.Add(postgresLogPipe); err != nil {
		return err
	}
	postgresStartConditions = append(postgresStartConditions, postgresLogPipe.GetInitializedCondition())
	exitedConditions = append(exitedConditions, postgresLogPipe.GetExitedCondition())

	// raw logs handler
	rawPipe := logpipe.NewRawLineLogPipe(filepath.Join(pg.LogPath, pg.LogFileName),
		logpipe.LoggingCollectorRecordName)
	if err := mgr.Add(rawPipe); err != nil {
		return err
	}
	postgresStartConditions = append(postgresStartConditions, rawPipe.GetExecutedCondition())
	exitedConditions = append(exitedConditions, rawPipe.GetExitedCondition())

	// json logs handler
	jsonPipe := logpipe.NewJSONLineLogPipe(filepath.Join(pg.LogPath, pg.LogFileName+".json"))
	if err := mgr.Add(jsonPipe); err != nil {
		return err
	}
	postgresStartConditions = append(postgresStartConditions, jsonPipe.GetExecutedCondition())
	exitedConditions = append(exitedConditions, jsonPipe.GetExitedCondition())

	postgresLifecycleManager := lifecycle.NewPostgres(ctx, instance, postgresStartConditions)
	if err = mgr.Add(postgresLifecycleManager); err != nil {
		setupLog.Error(err, "unable to create instance runnable")
		return err
	}

	if err = mgr.Add(metricsServer); err != nil {
		setupLog.Error(err, "unable to add metrics webserver runnable")
		return err
	}

	// onlineUpgradeCtx is a child context of the postgres context.
	// onlineUpgradeCtx will be the context passed to all the manager handled Runnables via Start(ctx),
	// its deletion will imply all Runnables to stop, but will be handled
	// appropriately by the Postgres Lifecycle Manager, which won't terminate Postgres in this case.
	// The parent GlobalContext will only be deleted by the Postgres Lifecycle Manager itself when required,
	// which will imply the deletion of the child onlineUpgradeCtx too, again, terminating all the Runnables.
	onlineUpgradeCtx, onlineUpgradeCancelFunc := context.WithCancel(postgresLifecycleManager.GetGlobalContext())
	defer onlineUpgradeCancelFunc()
	remoteSrv, err := webserver.NewRemoteWebServer(instance, onlineUpgradeCancelFunc, exitedConditions)
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
	if err := mgr.Start(onlineUpgradeCtx); err != nil {
		setupLog.Error(err, "unable to run controller-runtime manager")
		return makeUnretryableError(err)
	}

	return nil
}
