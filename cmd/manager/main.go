/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	postgresqlv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/cmd/manager/app"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/controllers"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/certs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/versions"
	// +kubebuilder:scaffold:imports
)

var (
	scheme         = runtime.NewScheme()
	setupLog       = ctrl.Log.WithName("setup")
	webhookCertDir = os.Getenv("WEBHOOK_CERT_DIR")
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

	_ = postgresqlv1alpha1.AddToScheme(scheme)
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

	// No subcommand invoked, let's start the operator
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	setupLog.Info("Starting Cloud Native PostgreSQL Operator", "version", versions.Version)

	watchNamespace := os.Getenv("WATCH_NAMESPACE")
	setupLog.Info("Listening for changes", "watchNamespace", watchNamespace)

	managerOptions := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "db9c8771.k8s.enterprisedb.io",
		Namespace:          watchNamespace,
		CertDir:            "/tmp",
	}
	if webhookCertDir != "" {
		// If OLM will generate certificates for us, let's just
		// use those
		managerOptions.CertDir = webhookCertDir
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
	if webhookCertDir != "" {
		// OLM is generating certificates for us so we can avoid
		// injecting/creating certificates
		certificatesGenerationFolder = ""
	}
	err = setupPKI(mgr.GetConfig(), certificatesGenerationFolder)

	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
		os.Exit(1)
	}

	if err = (&controllers.ClusterReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("Cluster"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}
	if err = (&controllers.BackupReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Backup"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		os.Exit(1)
	}
	if err = (&controllers.ScheduledBackupReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ScheduledBackup"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScheduledBackup")
		os.Exit(1)
	}

	if err = (&postgresqlv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupPKI(config *rest.Config, certDir string) error {
	/*
	   Ensure we have the required PKI infrastructure to make
	   the operator and the clusters working
	*/
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("cannot create a K8s client: %w", err)
	}

	pkiConfig := certs.PublicKeyInfrastructure{
		CaSecretName:                       controllers.CaSecretName,
		CertDir:                            certDir,
		SecretName:                         webhookSecretName,
		ServiceName:                        webhookServiceName,
		OperatorNamespace:                  controllers.GetOperatorNamespaceOrDie(),
		MutatingWebhookConfigurationName:   mutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: validatingWebhookConfigurationName,
	}
	err = pkiConfig.Setup(clientSet)
	if err != nil {
		return err
	}

	err = pkiConfig.SchedulePeriodicMaintenance(clientSet)
	if err != nil {
		return err
	}

	return nil
}
