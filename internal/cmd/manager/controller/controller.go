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

// Package controller implement the command used to start the operator
package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	webhookv1 "github.com/cloudnative-pg/cloudnative-pg/internal/webhook/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/multicache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

var (
	scheme   = schemeBuilder.BuildWithAllKnownScheme()
	setupLog = log.WithName("setup")
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

	// The name of the directory containing the TLS certificates for webhooks
	defaultWebhookCertDir = "/run/secrets/cnpg.io/webhook"

	// LeaderElectionID The operator Leader Election ID
	LeaderElectionID = "db9c8771.cnpg.io"

	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "cnpg-ca-secret" // #nosec

)

// leaderElectionConfiguration contains the leader parameters that will be passed to controllerruntime.Options.
type leaderElectionConfiguration struct {
	enable        bool
	leaseDuration time.Duration
	renewDeadline time.Duration
}

// RunController is the main procedure of the operator, and is used as the
// controller-manager of the operator and as the controller of a certain
// PostgreSQL instance.
//
// This code really belongs to app/controller_manager.go but we can't put
// it here to respect the project layout created by kubebuilder.
func RunController(
	metricsAddr string,
	configMapName string,
	secretName string,
	leaderConfig leaderElectionConfiguration,
	pprofDebug bool,
	port int,
	maxConcurrentReconciles int,
	conf *configuration.Data,
) error {
	ctx := context.Background()
	setupLog.Info("Starting CloudNativePG Operator",
		"version", versions.Version,
		"build", versions.Info)

	managerOptions := ctrl.Options{
		Scheme:           scheme,
		Metrics:          buildMetricsOpts(metricsAddr, conf),
		LeaderElection:   leaderConfig.enable,
		LeaseDuration:    &leaderConfig.leaseDuration,
		RenewDeadline:    &leaderConfig.renewDeadline,
		LeaderElectionID: LeaderElectionID,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    port,
			CertDir: defaultWebhookCertDir,
		}),
		PprofBindAddress: getPprofServerAddress(pprofDebug),
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

	if conf.WatchNamespace != "" {
		namespaces := conf.WatchedNamespaces()
		managerOptions.NewCache = multicache.DelegatingMultiNamespacedCacheBuilder(
			namespaces,
			conf.OperatorNamespace)
		setupLog.Info("Listening for changes", "watchNamespaces", namespaces)
	} else {
		setupLog.Info("Listening for changes on all namespaces")
	}

	if conf.WebhookCertDir != "" {
		// If OLM will generate certificates for us, let's just
		// use those
		managerOptions.WebhookServer.(*webhook.DefaultServer).Options.CertDir = conf.WebhookCertDir
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	webhookServer := mgr.GetWebhookServer().(*webhook.DefaultServer)
	if conf.WebhookCertDir != "" {
		// Use certificate names compatible with OLM
		webhookServer.Options.CertName = "apiserver.crt"
		webhookServer.Options.KeyName = "apiserver.key"
	} else {
		webhookServer.Options.CertName = "tls.crt"
		webhookServer.Options.KeyName = "tls.key"
	}

	// kubeClient is the kubernetes client set with
	// support for the apiextensions that is used
	// during the initialization of the operator
	// kubeClient client.Client
	kubeClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes client")
		return err
	}

	err = loadConfiguration(ctx, kubeClient, configMapName, secretName, conf)
	if err != nil {
		return err
	}

	setupLog.Info("Operator configuration loaded", "configuration", conf)

	// Validate configuration combination
	if err := conf.Validate(); err != nil {
		setupLog.Error(err, "invalid configuration")
		return err
	}

	discoveryClient, err := utils.GetDiscoveryClient()
	if err != nil {
		return err
	}

	// Detect if we are running under a system that implements OpenShift Security Context Constraints
	if err = utils.DetectSecurityContextConstraints(discoveryClient); err != nil {
		setupLog.Error(err, "unable to detect OpenShift Security Context Constraints presence")
		return err
	}

	// Detect if we are running under a system that provides Volume Snapshots
	if err = utils.DetectVolumeSnapshotExist(discoveryClient); err != nil {
		setupLog.Error(err, "unable to detect the if the cluster have the VolumeSnapshot CRD installed")
		return err
	}

	// Detect the available architectures
	if err = utils.DetectAvailableArchitectures(); err != nil {
		setupLog.Error(err, "unable to detect the available instance's architectures")
		return err
	}

	setupLog.Info("Kubernetes system metadata",
		"haveSCC", utils.HaveSecurityContextConstraints(),
		"haveVolumeSnapshot", utils.HaveVolumeSnapshot(),
		"availableArchitectures", utils.GetAvailableArchitectures(),
	)

	if err := ensurePKI(ctx, kubeClient, webhookServer.Options.CertDir, conf); err != nil {
		return err
	}

	pluginRepository := repository.New()
	if _, err := pluginRepository.RegisterUnixSocketPluginsInPath(
		conf.PluginSocketDir,
	); err != nil {
		setupLog.Error(err, "Unable to load sidecar CNPG-i plugins, skipping")
	}
	defer pluginRepository.Close()

	if err = controller.NewClusterReconciler(
		mgr,
		discoveryClient,
		pluginRepository,
		conf.DrainTaints,
	).SetupWithManager(ctx, mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		return err
	}

	if err = controller.NewBackupReconciler(
		mgr,
		discoveryClient,
		pluginRepository,
	).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		return err
	}

	if err = controller.NewPluginReconciler(mgr, conf.OperatorNamespace, pluginRepository).
		SetupWithManager(mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Plugin")
		return err
	}

	if err = (&controller.ScheduledBackupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("cloudnative-pg-scheduledbackup"), //nolint:staticcheck
	}).SetupWithManager(ctx, mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScheduledBackup")
		return err
	}

	if err = (&controller.PoolerReconciler{
		Client:          mgr.GetClient(),
		DiscoveryClient: discoveryClient,
		Scheme:          mgr.GetScheme(),
		Recorder:        mgr.GetEventRecorderFor("cloudnative-pg-pooler"), //nolint:staticcheck
	}).SetupWithManager(mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pooler")
		return err
	}

	if err = webhookv1.SetupClusterWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster", "version", "v1")
		return err
	}

	if err = webhookv1.SetupBackupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Backup", "version", "v1")
		return err
	}

	if err = webhookv1.SetupScheduledBackupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScheduledBackup", "version", "v1")
		return err
	}

	if err = webhookv1.SetupPoolerWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Pooler", "version", "v1")
		return err
	}

	if err = webhookv1.SetupDatabaseWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Database", "version", "v1")
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
	webhookServer.WebhookMux().HandleFunc("/readyz", readinessProbeHandler)

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}

func buildMetricsOpts(metricsAddr string, conf *configuration.Data) server.Options {
	metricsOptions := server.Options{
		BindAddress: metricsAddr,
	}

	if conf.MetricsCertDir == "" {
		setupLog.Info("Metrics server enabled without TLS")
		return metricsOptions
	}

	metricsOptions.CertName = "tls.crt"
	metricsOptions.KeyName = "tls.key"
	metricsOptions.SecureServing = true
	metricsOptions.CertDir = conf.MetricsCertDir
	setupLog.Info("Metrics server enabled with TLS", "certDir", conf.MetricsCertDir)

	return metricsOptions
}

// loadConfiguration reads the configuration from the provided configmap and secret
func loadConfiguration(
	ctx context.Context,
	kubeClient client.Client,
	configMapName string,
	secretName string,
	conf *configuration.Data,
) error {
	configData := make(map[string]string)

	// First read the configmap if provided and store it in configData
	if configMapName != "" {
		configMapData, err := readConfigMap(ctx, kubeClient, conf.OperatorNamespace, configMapName)
		if err != nil {
			setupLog.Error(err, "unable to read ConfigMap",
				"namespace", conf.OperatorNamespace,
				"name", configMapName)
			return err
		}
		for k, v := range configMapData {
			configData[k] = v
		}
	}

	// Then read the secret if provided and store it in configData, overwriting configmap's values
	if secretName != "" {
		secretData, err := readSecret(ctx, kubeClient, conf.OperatorNamespace, secretName)
		if err != nil {
			setupLog.Error(err, "unable to read Secret",
				"namespace", conf.OperatorNamespace,
				"name", secretName)
			return err
		}
		for k, v := range secretData {
			configData[k] = v
		}
	}

	// Finally, read the config if it was provided
	if len(configData) > 0 {
		conf.ReadConfigMap(configData)
	}

	return nil
}

// readinessProbeHandler is used to implement the readiness probe handler
func readinessProbeHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprint(w, "OK")
}

// ensurePKI ensures that we have the required PKI infrastructure to make
// the operator and the clusters working
func ensurePKI(
	ctx context.Context,
	kubeClient client.Client,
	mgrCertDir string,
	conf *configuration.Data,
) error {
	if conf.WebhookCertDir != "" {
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
		OperatorNamespace:                  conf.OperatorNamespace,
		MutatingWebhookConfigurationName:   MutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: ValidatingWebhookConfigurationName,
		OperatorDeploymentLabelSelector:    "app.kubernetes.io/name=cloudnative-pg",
	}
	err := pkiConfig.Setup(ctx, kubeClient)
	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
	}
	return err
}

// readConfigMap reads the configMap and returns its content as map
func readConfigMap(
	ctx context.Context,
	kubeClient client.Client,
	namespace string,
	name string,
) (map[string]string, error) {
	if name == "" {
		return nil, nil
	}

	if namespace == "" {
		return nil, nil
	}

	setupLog.Info("Loading configuration from ConfigMap",
		"namespace", namespace,
		"name", name)

	configMap := &corev1.ConfigMap{}
	err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, configMap)
	if apierrs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return configMap.Data, nil
}

// readSecret reads the secret and returns its content as map
func readSecret(
	ctx context.Context,
	kubeClient client.Client,
	namespace,
	name string,
) (map[string]string, error) {
	if name == "" {
		return nil, nil
	}

	if namespace == "" {
		return nil, nil
	}

	setupLog.Info("Loading configuration from Secret",
		"namespace", namespace,
		"name", name)

	secret := &corev1.Secret{}
	err := kubeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
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

func getPprofServerAddress(enabled bool) string {
	if enabled {
		return "0.0.0.0:6060"
	}

	return ""
}
