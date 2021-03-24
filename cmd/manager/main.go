/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/cmd/manager/app"
	"github.com/EnterpriseDB/cloud-native-postgresql/controllers"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// clientset is the kubernetes client used during
	// the initialization of the operator
	clientSet *kubernetes.Clientset

	// apiClientset is the kubernetes client set with
	// support for the apiextensions that is used
	// during the initialization of the operator
	apiClientSet *apiextensionsclientset.Clientset
)

const (
	// This is the name of the secret where the certificates
	// for the webhook server are stored
	webhookSecretName = "postgresql-operator-webhook-cert" // #nosec

	// This is the name of the service where the webhook server
	// is reachable
	webhookServiceName = "postgresql-operator-webhook-service" // #nosec

	// This is the name of the mutating webhook configuration
	mutatingWebhookConfigurationName = "postgresql-operator-mutating-webhook-configuration"

	// This is the name of the validating webhook configuration
	validatingWebhookConfigurationName = "postgresql-operator-validating-webhook-configuration"
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = apiv1alpha1.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

// This is the main procedure of the operator, and is used as the
// controller-manager of the operator and as the controller of a certain
// PostgreSQL instance.
//
// This code really belongs to app/controller_manager.go but we can't put
// it here to respect the project layout created by kubebuilder.
//
// TODO this code wants to be replaced by using Cobra. Please evaluate if
// there are cons using Cobra with kubebuilder
func main() {
	// If we are about to handle a subcommand, let's do that
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "instance":
			app.InstanceManagerCommand(os.Args[2:])
			return

		case "bootstrap":
			app.BootstrapIntoCommand(os.Args[0], os.Args[2:])
			return

		case "wal-archive":
			app.WalArchiveCommand(os.Args[2:])
			return

		case "wal-restore":
			app.WalRestoreCommand(os.Args[2:])
			return

		case "backup":
			app.BackupCommand(os.Args[2:])
			return
		}
	}

	ctx := context.Background()

	// No subcommand invoked, let's start the operator
	var metricsAddr string
	var configMapName string
	var enableLeaderElection bool
	var opts zap.Options
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&configMapName, "config-map-name", "", "The name of the ConfigMap")
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Starting Cloud Native PostgreSQL Operator", "version", versions.Version)

	setupLog.Info("Listening for changes", "watchNamespace", configuration.GetWatchNamespace())

	managerOptions := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "db9c8771.k8s.enterprisedb.io",
		Namespace:          configuration.GetWatchNamespace(),
		CertDir:            "/tmp",
	}
	if configuration.GetWebhookCertDir() != "" {
		// If OLM will generate certificates for us, let's just
		// use those
		managerOptions.CertDir = configuration.GetWebhookCertDir()
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Use certificate names compatible with OLM
	mgr.GetWebhookServer().CertName = "apiserver.crt"
	mgr.GetWebhookServer().KeyName = "apiserver.key"

	certificatesGenerationFolder := mgr.GetWebhookServer().CertDir
	if configuration.GetWebhookCertDir() != "" {
		// OLM is generating certificates for us so we can avoid
		// injecting/creating certificates
		certificatesGenerationFolder = ""
	}

	err = createKubernetesClient(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes clients")
		os.Exit(1)
	}

	if configMapName != "" {
		err := readConfigMap(ctx, configuration.GetOperatorNamespace(), configMapName)
		if err != nil {
			setupLog.Error(err, "unable to read ConfigMap",
				"namespace", configuration.GetOperatorNamespace(),
				"name", configMapName)
		}
	}

	err = setupPKI(ctx, certificatesGenerationFolder)
	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
		os.Exit(1)
	}

	if err = (&controllers.ClusterReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("Cluster"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}
	if err = (&controllers.BackupReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("Backup"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql-backup"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		os.Exit(1)
	}
	if err = (&controllers.ScheduledBackupReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("ScheduledBackup"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql-scheduledbackup"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScheduledBackup")
		os.Exit(1)
	}

	if err = (&apiv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1alpha1")
		os.Exit(1)
	}

	if err = (&apiv1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1")
		os.Exit(1)
	}

	if err = (&apiv1alpha1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1alpha1")
		os.Exit(1)
	}

	if err = (&apiv1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1")
		os.Exit(1)
	}

	if err = (&apiv1alpha1.ScheduledBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1alpha1")
		os.Exit(1)
	}

	if err = (&apiv1.ScheduledBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// createKubernetesClient creates the Kubernetes client that will be used during
// the operator initialization
func createKubernetesClient(config *rest.Config) error {
	var err error
	clientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("cannot create a K8s client: %w", err)
	}

	apiClientSet, err = apiextensionsclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("cannot create a K8s API extension client: %w", err)
	}

	return nil
}

// setupPKI ensures that we have the required PKI infrastructure to make
// the operator and the clusters working
func setupPKI(ctx context.Context, certDir string) error {
	pkiConfig := certs.PublicKeyInfrastructure{
		CaSecretName:                       controllers.CaSecretName,
		CertDir:                            certDir,
		SecretName:                         webhookSecretName,
		ServiceName:                        webhookServiceName,
		OperatorNamespace:                  configuration.GetOperatorNamespace(),
		MutatingWebhookConfigurationName:   mutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: validatingWebhookConfigurationName,
		CustomResourceDefinitionsName: []string{
			"backups.postgresql.k8s.enterprisedb.io",
			"clusters.postgresql.k8s.enterprisedb.io",
			"scheduledbackups.postgresql.k8s.enterprisedb.io",
		},
	}
	err := pkiConfig.Setup(ctx, clientSet, apiClientSet)
	if err != nil {
		return err
	}

	err = pkiConfig.SchedulePeriodicMaintenance(ctx, clientSet, apiClientSet)
	if err != nil {
		return err
	}

	return nil
}

// readConfigMap reads the configMap
func readConfigMap(ctx context.Context, namespace, name string) error {
	if name == "" {
		return nil
	}

	if namespace == "" {
		return nil
	}

	setupLog.Info("Loading configuration from ConfigMap",
		"namespace", namespace,
		"name", name)

	configMap, err := clientSet.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	configuration.ReadConfigMap(configMap.Data)

	return nil
}
