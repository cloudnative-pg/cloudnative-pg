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

// Package controller implement the command used to start the operator
package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/controllers"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/multicache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = log.WithName("setup")

	// clientSet is the kubernetes client used during
	// the initialization of the operator
	clientSet *kubernetes.Clientset

	// apiClientSet is the kubernetes client set with
	// support for the apiextensions that is used
	// during the initialization of the operator
	apiClientSet *apiextensionsclientset.Clientset
)

const (
	// WebhookSecretName is the name of the secret where the certificates
	// for the webhook server are stored
	WebhookSecretName = "cnpg-webhook-cert" // #nosec

	// WebhookServiceName is the name of the service where the webhook server
	// is reachable
	WebhookServiceName = "cnpg-webhook-service" // #nosec

	// MutatingWebhookConfigurationName is the name of the mutating webhook configuration
	MutatingWebhookConfigurationName = "cnpg-mutating-webhook-configuration"

	// ValidatingWebhookConfigurationName is the name of the validating webhook configuration
	ValidatingWebhookConfigurationName = "cnpg-validating-webhook-configuration"

	// The name of the directory containing the TLS certificates
	defaultWebhookCertDir = "/run/secrets/cnpg.io/webhook"

	// LeaderElectionID The operator Leader Election ID
	LeaderElectionID = "db9c8771.cnpg.io"

	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "cnpg-ca-secret" // #nosec

)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
	_ = monitoringv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

// RunController is the main procedure of the operator, and is used as the
// controller-manager of the operator and as the controller of a certain
// PostgreSQL instance.
//
// This code really belongs to app/controller_manager.go but we can't put
// it here to respect the project layout created by kubebuilder.
func RunController(
	metricsAddr,
	configMapName,
	secretName string,
	enableLeaderElection,
	pprofDebug bool,
	port int,
) error {
	ctx := context.Background()

	setupLog.Info("Starting CloudNativePG Operator",
		"version", versions.Version,
		"build", versions.Info)

	if pprofDebug {
		startPprofDebugServer(ctx)
	}

	managerOptions := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               port,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   LeaderElectionID,
		CertDir:            defaultWebhookCertDir,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		LeaderElectionReleaseOnCancel: true,
	}

	if configuration.Current.WatchNamespace != "" {
		namespaces := configuration.Current.WatchedNamespaces()
		managerOptions.NewCache = multicache.DelegatingMultiNamespacedCacheBuilder(
			namespaces,
			configuration.Current.OperatorNamespace)
		setupLog.Info("Listening for changes", "watchNamespaces", namespaces)
	} else {
		setupLog.Info("Listening for changes on all namespaces")
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

	if configuration.Current.WebhookCertDir != "" {
		// Use certificate names compatible with OLM
		mgr.GetWebhookServer().CertName = "apiserver.crt"
		mgr.GetWebhookServer().KeyName = "apiserver.key"
	} else {
		mgr.GetWebhookServer().CertName = "tls.crt"
		mgr.GetWebhookServer().KeyName = "tls.key"
	}

	err = createKubernetesClient(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes clients")
		return err
	}

	err = loadConfiguration(ctx, configMapName, secretName)
	if err != nil {
		return err
	}

	setupLog.Info("Operator configuration loaded", "configuration", configuration.Current)

	discoveryClient, err := utils.GetDiscoveryClient()
	if err != nil {
		return err
	}

	// Detect if we are running under a system that implements OpenShift Security Context Constraints
	if err = utils.DetectSecurityContextConstraints(discoveryClient); err != nil {
		setupLog.Error(err, "unable to detect OpenShift Security Context Constraints presence")
		return err
	}

	// Retrieve the Kubernetes cluster system UID
	if err = utils.DetectKubeSystemUID(ctx, clientSet); err != nil {
		setupLog.Error(err, "unable to retrieve the Kubernetes cluster system UID")
		return err
	}

	setupLog.Info("Kubernetes system metadata",
		"systemUID", utils.GetKubeSystemUID(),
		"haveSCC", utils.HaveSecurityContextConstraints())

	if err := ensurePKI(ctx, mgr.GetWebhookServer().CertDir); err != nil {
		return err
	}

	if err = controllers.NewClusterReconciler(mgr, discoveryClient).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		return err
	}

	if err = (&controllers.BackupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloudnative-pg-backup"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		return err
	}

	if err = (&controllers.ScheduledBackupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloudnative-pg-scheduledbackup"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScheduledBackup")
		return err
	}

	if err = (&controllers.PoolerReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloudnative-pg-pooler"),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pooler")
		return err
	}

	if err = (&apiv1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1")
		return err
	}

	if err = (&apiv1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1")
		return err
	}

	if err = (&apiv1.ScheduledBackup{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1")
		return err
	}

	if err = (&apiv1.Pooler{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Pooler", "version", "v1")
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

// loadConfiguration reads the configuration from the provided configmap and secret
func loadConfiguration(ctx context.Context, configMapName string, secretName string) error {
	configData := make(map[string]string)

	// First read the configmap if provided and store it in configData
	if configMapName != "" {
		configMapData, err := readConfigMap(ctx, configuration.Current.OperatorNamespace, configMapName)
		if err != nil {
			setupLog.Error(err, "unable to read ConfigMap",
				"namespace", configuration.Current.OperatorNamespace,
				"name", configMapName)
			return err
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
			return err
		}
		for k, v := range secretData {
			configData[k] = v
		}
	}

	// Finally, read the config if it was provided
	if len(configData) > 0 {
		configuration.Current.ReadConfigMap(configData)
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

// ensurePKI ensures that we have the required PKI infrastructure to make
// the operator and the clusters working
func ensurePKI(ctx context.Context, mgrCertDir string) error {
	if configuration.Current.WebhookCertDir != "" {
		// OLM is generating certificates for us, so we can avoid injecting/creating certificates.
		return nil
	}

	// We need to self-manage required PKI infrastructure and install the certificates into
	// the webhooks configuration
	pkiConfig := certs.PublicKeyInfrastructure{
		CaSecretName:                       CaSecretName,
		CertDir:                            mgrCertDir,
		SecretName:                         WebhookSecretName,
		ServiceName:                        WebhookServiceName,
		OperatorNamespace:                  configuration.Current.OperatorNamespace,
		MutatingWebhookConfigurationName:   MutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: ValidatingWebhookConfigurationName,
		CustomResourceDefinitionsName: []string{
			"backups.postgresql.cnpg.io",
			"clusters.postgresql.cnpg.io",
			"scheduledbackups.postgresql.cnpg.io",
		},
		OperatorDeploymentLabelSelector: "app.kubernetes.io/name=cloudnative-pg",
	}
	err := pkiConfig.Setup(ctx, clientSet, apiClientSet)
	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
	}
	return err
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

// startPprofDebugServer exposes pprof debug server if POD_DEBUG env variable is set to 1
func startPprofDebugServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	pprofServer := http.Server{
		Addr:              "0.0.0.0:6060",
		Handler:           mux,
		ReadTimeout:       webserver.DefaultReadTimeout,
		ReadHeaderTimeout: webserver.DefaultReadHeaderTimeout,
	}

	setupLog.Info("Starting pprof HTTP server", "addr", pprofServer.Addr)

	go func() {
		go func() {
			<-ctx.Done()

			setupLog.Info("shutting down pprof HTTP server")
			ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelFunc()

			if err := pprofServer.Shutdown(ctx); err != nil {
				setupLog.Error(err, "Failed to shutdown pprof HTTP server")
			}
		}()

		if err := pprofServer.ListenAndServe(); !errors.Is(http.ErrServerClosed, err) {
			setupLog.Error(err, "Failed to start pprof HTTP server")
		}
	}()
}
