/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controller implement the command used to start the operator
package controller

import (
	"context"
	"fmt"
	"net/http"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/controllers"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
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
	// WebhookSecretName is the name of the secret where the certificates
	// for the webhook server are stored
	WebhookSecretName = "postgresql-operator-webhook-cert" // #nosec

	// WebhookServiceName is the name of the service where the webhook server
	// is reachable
	WebhookServiceName = "postgresql-operator-webhook-service" // #nosec

	// MutatingWebhookConfigurationName is the name of the mutating webhook configuration
	MutatingWebhookConfigurationName = "postgresql-operator-mutating-webhook-configuration"

	// ValidatingWebhookConfigurationName is the name of the validating webhook configuration
	ValidatingWebhookConfigurationName = "postgresql-operator-validating-webhook-configuration"

	// The name of the directory containing the TLS certificates
	defaultWebhookCertDir = "/controller/certificates"

	// LeaderElectionID The operator Leader Election ID
	LeaderElectionID = "db9c8771.k8s.enterprisedb.io"

	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "postgresql-operator-ca-secret" // #nosec

)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = apiv1alpha1.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

// RunController is the main procedure of the operator, and is used as the
// controller-manager of the operator and as the controller of a certain
// PostgreSQL instance.
//
// This code really belongs to app/controller_manager.go but we can't put
// it here to respect the project layout created by kubebuilder.
func RunController(metricsAddr, configMapName, secretName string, enableLeaderElection bool, port int) error {
	ctx := context.Background()

	setupLog.Info("Starting Cloud Native PostgreSQL Operator",
		"version", versions.Version,
		"build", versions.Info)
	setupLog.Info("Listening for changes", "watchNamespace", configuration.Current.WatchNamespace)

	managerOptions := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               port,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   LeaderElectionID,
		Namespace:          configuration.Current.WatchNamespace,
		CertDir:            defaultWebhookCertDir,
	}
	if configuration.Current.WebhookCertDir != "" {
		// If OLM will generate certificates for us, let's just
		// use those
		managerOptions.CertDir = configuration.Current.WebhookCertDir
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	// Use certificate names compatible with OLM
	mgr.GetWebhookServer().CertName = "apiserver.crt"
	mgr.GetWebhookServer().KeyName = "apiserver.key"

	certificatesGenerationFolder := mgr.GetWebhookServer().CertDir
	if configuration.Current.WebhookCertDir != "" {
		// OLM is generating certificates for us so we can avoid
		// injecting/creating certificates
		certificatesGenerationFolder = ""
	}

	err = createKubernetesClient(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes clients")
		return err
	}

	// Read configurations
	configData := make(map[string]string)
	// First read the configmap if provided and store it in configData
	if configMapName != "" {
		configMapData, err := readConfigMap(ctx, configuration.Current.OperatorNamespace, configMapName)
		if err != nil {
			setupLog.Error(err, "unable to read ConfigMap",
				"namespace", configuration.Current.OperatorNamespace,
				"name", configMapName)
		}
		for k, v := range configMapData {
			configData[k] = v
		}
	}

	// Then read the secret if provided and store it in configData, overwriting configmap's values
	if secretName != "" {
		secretData, err := readSecret(ctx, configuration.Current.OperatorNamespace, secretName)
		if err != nil {
			setupLog.Error(err, "unable to read Secret",
				"namespace", configuration.Current.OperatorNamespace,
				"name", secretName)
		}
		for k, v := range secretData {
			configData[k] = v
		}
	}

	// Finally read the config if it was provided
	if len(configData) > 0 {
		configuration.Current.ReadConfigMap(configData)
	}

	setupLog.Info("Operator configuration loaded", "configuration", configuration.Current)

	// Detect if we are running under a system that implements OpenShift Security Context Constraints
	if err := utils.DetectSecurityContextConstraints(); err != nil {
		setupLog.Error(err, "unable to detect OpenShift Security Context Constraints presence")
		return err
	}

	// Retrieve the Kubernetes cluster system UID
	if err := utils.DetectKubeSystemUID(ctx, clientSet); err != nil {
		setupLog.Error(err, "unable to retrieve the Kubernetes cluster system UID")
		return err
	}

	setupLog.Info("Kubernetes system metadata",
		"systemUID", utils.GetKubeSystemUID(),
		"haveSCC", utils.HaveSecurityContextConstraints())

	err = setupPKI(ctx, certificatesGenerationFolder)
	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
		return err
	}

	if err = (&controllers.ClusterReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("Cluster"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		return err
	}
	if err = (&controllers.BackupReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("Backup"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql-backup"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		return err
	}
	if err = (&controllers.ScheduledBackupReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("ScheduledBackup"),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloud-native-postgresql-scheduledbackup"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScheduledBackup")
		return err
	}

	if err = (&apiv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1alpha1")
		return err
	}

	if err = (&apiv1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1")
		return err
	}

	if err = (&apiv1alpha1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1alpha1")
		return err
	}

	if err = (&apiv1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1")
		return err
	}

	if err = (&apiv1alpha1.ScheduledBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1alpha1")
		return err
	}

	if err = (&apiv1.ScheduledBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1")
		return err
	}

	// Setup the handler used by the readiness and liveliness probe.
	//
	// Unfortunately the readiness of the probe is not sufficient for the operator to be
	// working correctly. The probe may be positive even when:
	//
	// 1. the CA is not yet updated inside the CRD and/or in the validating/mutating
	//    webhook configuration. In that case we have a timeout error after trying
	//    to send a POST message and getting no response message.
	//
	// 2. the webhook service and/or the CNI are being updated, e.g. when a POD is
	//    deleted. In that case we could get a "Connection refused" error message.
	mgr.GetWebhookServer().WebhookMux.HandleFunc("/readyz", readinessProbeHandler)

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}

// readinessProbeHandler is used to implement the readiness probe handler
func readinessProbeHandler(w http.ResponseWriter, _r *http.Request) {
	_, _ = fmt.Fprint(w, "OK")
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
		CaSecretName:                       CaSecretName,
		CertDir:                            certDir,
		SecretName:                         WebhookSecretName,
		ServiceName:                        WebhookServiceName,
		OperatorNamespace:                  configuration.Current.OperatorNamespace,
		MutatingWebhookConfigurationName:   MutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: ValidatingWebhookConfigurationName,
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

// readConfigMap reads the configMap and returns its content as map
func readConfigMap(ctx context.Context, namespace, name string) (map[string]string, error) {
	if name == "" {
		return nil, nil
	}

	if namespace == "" {
		return nil, nil
	}

	setupLog.Info("Loading configuration from ConfigMap",
		"namespace", namespace,
		"name", name)

	configMap, err := clientSet.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return configMap.Data, nil
}

// readSecret reads the secret and returns its content as map
func readSecret(ctx context.Context, namespace, name string) (map[string]string, error) {
	if name == "" {
		return nil, nil
	}

	if namespace == "" {
		return nil, nil
	}

	setupLog.Info("Loading configuration from Secret",
		"namespace", namespace,
		"name", name)

	secret, err := clientSet.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for k, v := range secret.Data {
		data[k] = string(v)
	}

	return data, nil
}
