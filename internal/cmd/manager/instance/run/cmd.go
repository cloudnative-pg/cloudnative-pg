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

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/run/lifecycle"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/externalservers"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/roles"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/runner"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/metrics"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
	pg "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	instancestorage "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/storage"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

var (
	scheme = runtime.NewScheme()

	// errNoFreeWALSpace is returned when there isn't enough disk space
	// available to store at least two WAL files.
	errNoFreeWALSpace = fmt.Errorf("no free disk space for WALs")

	// errWALArchivePluginNotAvailable is returned when the configured
	// WAL archiving plugin is not available or cannot be found.
	errWALArchivePluginNotAvailable = fmt.Errorf("WAL archive plugin not available")
)

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
	var pprofHTTPServer bool
	var statusPortTLS bool
	var metricsPortTLS bool

	cmd := &cobra.Command{
		Use: "run [flags]",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), client.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := log.IntoContext(
				cmd.Context(),
				log.GetLogger().WithValues("logger", "instance-manager"),
			)

			checkCurrentOSRelease(ctx)

			instance := postgres.NewInstance().
				WithPodName(podName).
				WithClusterName(clusterName).
				WithNamespace(namespace)

			instance.PgData = pgData
			instance.StatusPortTLS = statusPortTLS
			instance.MetricsPortTLS = metricsPortTLS

			// Since version 0.19.0 of controller-runtime, it is not allowed to create multiple controllers with the
			// same name. As this part of the code is run inside a retry block, we need to allow SkipNameValidation
			// only on retries, because a previous run may have already created a controller
			// Reference https://github.com/kubernetes-sigs/controller-runtime/releases/tag/v0.19.0
			var skipNameValidation bool
			err := retry.OnError(retry.DefaultRetry, isRunSubCommandRetryable, func() error {
				defer func() { skipNameValidation = true }()
				return runSubCommand(ctx, instance, pprofHTTPServer, skipNameValidation)
			})

			if errors.Is(err, errNoFreeWALSpace) {
				os.Exit(apiv1.MissingWALDiskSpaceExitCode)
			}
			if errors.Is(err, errWALArchivePluginNotAvailable) {
				os.Exit(apiv1.MissingWALArchivePlugin)
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

	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be started up")
	cmd.Flags().StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().BoolVar(
		&pprofHTTPServer,
		"pprof-server",
		false,
		"If true it will start a pprof debug http server on localhost:6060. Defaults to false.",
	)
	cmd.Flags().BoolVar(&statusPortTLS, "status-port-tls", false,
		"Enable TLS for communicating with the operator")
	cmd.Flags().BoolVar(&metricsPortTLS, "metrics-port-tls", false,
		"Enable TLS for metrics scraping")
	return cmd
}

func runSubCommand( //nolint:gocognit,gocyclo
	ctx context.Context,
	instance *postgres.Instance,
	pprofServer bool,
	skipNameValidation bool,
) error {
	var err error

	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Starting CloudNativePG Instance Manager",
		"version", versions.Version,
		"build", versions.Info,
		"skipNameValidation", skipNameValidation)

	contextLogger.Info("Checking for free disk space for WALs before starting PostgreSQL")
	hasDiskSpaceForWals, err := instance.CheckHasDiskSpaceForWAL(ctx)
	if err != nil {
		contextLogger.Error(err, "Error while checking if there is enough disk space for WALs, skipping")
	} else if !hasDiskSpaceForWals {
		contextLogger.Info("Detected low-disk space condition, avoid starting the instance")
		return errNoFreeWALSpace
	}

	mgr, err := ctrl.NewManager(config.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&apiv1.Cluster{}: {
					Field: fields.OneTermEqualSelector("metadata.name", instance.GetClusterName()),
					Namespaces: map[string]cache.Config{
						instance.GetNamespaceName(): {},
					},
				},
				&apiv1.Database{}: {
					Namespaces: map[string]cache.Config{
						instance.GetNamespaceName(): {},
					},
				},
				&apiv1.Publication{}: {
					Namespaces: map[string]cache.Config{
						instance.GetNamespaceName(): {},
					},
				},
				&apiv1.Subscription{}: {
					Namespaces: map[string]cache.Config{
						instance.GetNamespaceName(): {},
					},
				},
			},
		},
		// We don't need a cache for secrets and configmap, as all reloads
		// should be driven by changes in the Cluster we are watching
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.Secret{},
					&corev1.ConfigMap{},
					// we don't have the permissions to cache backups, as the ServiceAccount
					// doesn't have watch permission on the backup status
					&apiv1.Backup{},
					// we don't have the permissions to cache FailoverQuorum objects, we can
					// only access the object having the same name as the cluster
					&apiv1.FailoverQuorum{},
				},
			},
		},
		Metrics: server.Options{
			BindAddress: "0", // TODO: merge metrics to the manager one
		},
		BaseContext: func() context.Context {
			return ctx
		},
		Logger:           contextLogger.WithValues("logging_pod", os.Getenv("POD_NAME")).GetLogger(),
		PprofBindAddress: getPprofServerAddress(pprofServer),
		Controller: ctrlconfig.Controller{
			SkipNameValidation: ptr.To(skipNameValidation),
		},
	})
	if err != nil {
		contextLogger.Error(err, "unable to set up overall controller manager")
		return err
	}

	postgresStartConditions := concurrency.MultipleExecuted{}
	exitedConditions := concurrency.MultipleExecuted{}

	var loadedPluginNames []string
	pluginRepository := repository.New()
	if loadedPluginNames, err = pluginRepository.RegisterUnixSocketPluginsInPath(
		configuration.Current.PluginSocketDir,
	); err != nil {
		contextLogger.Error(err, "Unable to load sidecar CNPG-i plugins, skipping")
	}
	defer pluginRepository.Close()

	metricsExporter := metricserver.NewExporter(instance, metrics.NewPluginCollector(pluginRepository))
	reconciler := controller.NewInstanceReconciler(instance, mgr.GetClient(), metricsExporter, pluginRepository)
	err = ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Named("instance-cluster").
		Complete(reconciler)
	if err != nil {
		contextLogger.Error(err, "unable to create instance controller")
		return err
	}
	postgresStartConditions = append(postgresStartConditions, reconciler.GetExecutedCondition())

	// database reconciler
	dbReconciler := controller.NewDatabaseReconciler(mgr, instance)
	if err := dbReconciler.SetupWithManager(mgr); err != nil {
		contextLogger.Error(err, "unable to create database controller")
		return err
	}

	// database publication reconciler
	publicationReconciler := controller.NewPublicationReconciler(mgr, instance)
	if err := publicationReconciler.SetupWithManager(mgr); err != nil {
		contextLogger.Error(err, "unable to create publication controller")
		return err
	}

	// database subscription reconciler
	subscriptionReconciler := controller.NewSubscriptionReconciler(mgr, instance)
	if err := subscriptionReconciler.SetupWithManager(mgr); err != nil {
		contextLogger.Error(err, "unable to create subscription controller")
		return err
	}

	// postgres CSV logs handler (PGAudit too)
	postgresLogPipe := logpipe.NewLogPipe()
	if err := mgr.Add(postgresLogPipe); err != nil {
		contextLogger.Error(err, "unable to add CSV logs handler")
		return err
	}
	postgresStartConditions = append(postgresStartConditions, postgresLogPipe.GetInitializedCondition())
	exitedConditions = append(exitedConditions, postgresLogPipe.GetExitedCondition())

	// raw logs handler
	rawPipe := logpipe.NewRawLineLogPipe(filepath.Join(pg.LogPath, pg.LogFileName),
		logpipe.LoggingCollectorRecordName)
	if err := mgr.Add(rawPipe); err != nil {
		contextLogger.Error(err, "unable to add raw logs handler")
		return err
	}
	postgresStartConditions = append(postgresStartConditions, rawPipe.GetExecutedCondition())
	exitedConditions = append(exitedConditions, rawPipe.GetExitedCondition())

	// json logs handler
	jsonPipe := logpipe.NewJSONLineLogPipe(filepath.Join(pg.LogPath, pg.LogFileName+".json"))
	if err := mgr.Add(jsonPipe); err != nil {
		contextLogger.Error(err, "unable to add JSON logs handler")
		return err
	}
	postgresStartConditions = append(postgresStartConditions, jsonPipe.GetExecutedCondition())
	exitedConditions = append(exitedConditions, jsonPipe.GetExitedCondition())

	if err := instancestorage.ReconcileWalDirectory(ctx); err != nil {
		contextLogger.Error(err, "unable to move `pg_wal` directory to the attached volume")
		return err
	}

	postgresLifecycleManager := lifecycle.NewPostgres(ctx, instance, postgresStartConditions)
	if err = mgr.Add(postgresLifecycleManager); err != nil {
		contextLogger.Error(err, "unable to create instance runnable")
		return err
	}

	if err = mgr.Add(lifecycle.NewPostgresOrphansReaper(instance)); err != nil {
		contextLogger.Error(err, "unable to create zombie reaper")
		return err
	}

	slotReplicator := runner.NewReplicator(instance)
	if err = mgr.Add(slotReplicator); err != nil {
		contextLogger.Error(err, "unable to create slot replicator")
		return err
	}

	roleSynchronizer := roles.NewRoleSynchronizer(instance, reconciler.GetClient())
	if err = mgr.Add(roleSynchronizer); err != nil {
		contextLogger.Error(err, "unable to create role synchronizer")
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
		contextLogger.Error(err, "unable to create remote webserver runnable")
		return err
	}
	if err = mgr.Add(remoteSrv); err != nil {
		contextLogger.Error(err, "unable to add remote webserver runnable")
		return err
	}

	localSrv, err := webserver.NewLocalWebServer(
		instance,
		mgr.GetClient(),
		mgr.GetEventRecorderFor("local-webserver"), //nolint:staticcheck
	)
	if err != nil {
		contextLogger.Error(err, "unable to create local webserver runnable")
		return err
	}
	if err = mgr.Add(localSrv); err != nil {
		contextLogger.Error(err, "unable to add local webserver runnable")
		return err
	}

	metricsServer, err := metricserver.New(instance, metricsExporter)
	if err != nil {
		contextLogger.Error(err, "unable to create metrics server runnable")
		return err
	}
	if err = mgr.Add(metricsServer); err != nil {
		contextLogger.Error(err, "unable to add metrics server runnable")
		return err
	}

	contextLogger.Info("starting tablespace manager")
	if err := tablespaces.NewTablespaceReconciler(instance, mgr.GetClient()).
		SetupWithManager(mgr); err != nil {
		contextLogger.Error(err, "unable to create tablespace reconciler")
		return err
	}

	contextLogger.Info("starting external server manager")
	if err := externalservers.NewReconciler(instance, mgr.GetClient()).
		SetupWithManager(mgr); err != nil {
		contextLogger.Error(err, "unable to create external servers reconciler")
		return err
	}

	contextLogger.Info("starting controller-runtime manager")
	if err := mgr.Start(onlineUpgradeCtx); err != nil {
		contextLogger.Error(err, "unable to run controller-runtime manager")
		return makeUnretryableError(err)
	}

	contextLogger.Info("Checking for free disk space for WALs after PostgreSQL finished")
	hasDiskSpaceForWals, err = instance.CheckHasDiskSpaceForWAL(ctx)
	if err != nil {
		contextLogger.Error(err, "Error while checking if there is enough disk space for WALs, skipping")
	} else if !hasDiskSpaceForWals {
		contextLogger.Info("Detected low-disk space condition")
		return makeUnretryableError(errNoFreeWALSpace)
	}

	enabledArchiverPluginName := instance.Cluster.GetEnabledWALArchivePluginName()
	if enabledArchiverPluginName != "" && !slices.Contains(loadedPluginNames, enabledArchiverPluginName) {
		contextLogger.Info(
			"Detected missing WAL archiver plugin, waiting for the operator to rollout a new instance Pod",
			"enabledArchiverPluginName", enabledArchiverPluginName,
			"loadedPluginNames", loadedPluginNames)
		return makeUnretryableError(errWALArchivePluginNotAvailable)
	}

	return nil
}

func getPprofServerAddress(enabled bool) string {
	if enabled {
		return "0.0.0.0:6060"
	}

	return ""
}
