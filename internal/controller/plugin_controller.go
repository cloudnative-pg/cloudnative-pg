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

// Package controller contains the controller of the CRD
package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PluginReconciler reconciles CNPG-i plugins
type PluginReconciler struct {
	client.Client

	Scheme  *runtime.Scheme
	Plugins repository.Interface

	OperatorNamespace string
}

// NewPluginReconciler creates a new PluginReconciler initializing it
func NewPluginReconciler(
	mgr manager.Manager,
	operatorNamespace string,
	plugins repository.Interface,
) *PluginReconciler {
	return &PluginReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Plugins:           plugins,
		OperatorNamespace: operatorNamespace,
	}
}

// Reconcile is the reconciler loop
func (r *PluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Trace("Plugin reconciliation loop start")
	defer func() {
		contextLogger.Trace("Plugin reconciliation loop end")
	}()

	var service corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		// This also happens when you delete a resource in k8s
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, fmt.Errorf("cannot get the resource: %w", err)
	}

	if !isPluginService(&service, r.OperatorNamespace) {
		contextLogger.Trace("Skipping reconciliation for a non-cnpg-i service")
		return ctrl.Result{}, nil
	}

	// Process label and annotations
	pluginName := service.Labels[utils.PluginNameLabelName]
	if len(pluginName) == 0 {
		contextLogger.Info("Detected service whose plugin name label is empty, skipping")
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !service.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.handleDeletion(ctx, &service, pluginName)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&service, utils.PluginFinalizerName) {
		contextLogger.Debug("Adding finalizer to plugin service")
		controllerutil.AddFinalizer(&service, utils.PluginFinalizerName)
		if err := r.Update(ctx, &service); err != nil {
			contextLogger.Error(err, "Error while adding finalizer to plugin service")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	res, err := r.reconcile(ctx, &service, pluginName)
	if err != nil {
		r.Plugins.ForgetPlugin(pluginName)
		return ctrl.Result{}, err
	}

	return res, nil
}

// nolint:unparam
func (r *PluginReconciler) reconcile(
	ctx context.Context,
	service *corev1.Service,
	pluginName string,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithValues(
		"pluginName", pluginName,
		"service", client.ObjectKeyFromObject(service))
	contextLogger.Debug("Plugin reconciliation loop start")
	defer func() {
		contextLogger.Debug("Plugin reconciliation loop end")
	}()

	pluginServerSecret := service.Annotations[utils.PluginServerSecretAnnotationName]
	if len(pluginServerSecret) == 0 {
		contextLogger.Info("Detected service whose server secret annotation is empty, skipping")
		return ctrl.Result{}, nil
	}
	serverSecret, err := r.getSecret(ctx, client.ObjectKey{
		Namespace: service.Namespace,
		Name:      pluginServerSecret,
	})
	if err != nil {
		contextLogger.Error(err, "Error while getting server secret for plugin",
			"secretName", pluginServerSecret)
		return ctrl.Result{}, err
	}

	pluginClientSecret := service.Annotations[utils.PluginClientSecretAnnotationName]
	if len(pluginClientSecret) == 0 {
		contextLogger.Info("Detected service whose client secret annotation is empty, skipping")
		return ctrl.Result{}, nil
	}
	clientSecret, err := r.getSecret(ctx, client.ObjectKey{
		Namespace: service.Namespace,
		Name:      pluginClientSecret,
	})
	if err != nil {
		contextLogger.Error(err, "Error while getting client secret for plugin",
			"secretName", pluginClientSecret)
		return ctrl.Result{}, err
	}

	pluginPortString := service.Annotations[utils.PluginPortAnnotationName]
	if len(pluginPortString) == 0 {
		contextLogger.Info("Detected service whose plugin port annotation is empty, skipping")
		return ctrl.Result{}, nil
	}
	pluginPort, err := strconv.Atoi(pluginPortString)
	if err != nil {
		err = fmt.Errorf("while parsing plugin server port: %w", err)
		contextLogger.Error(
			err,
			"Detected service whose plugin port annotation content is not correct, retrying",
			"pluginPortString", pluginPortString,
		)
		return ctrl.Result{}, err
	}

	// Create the plugin TLS configuration
	clientKeyPair, err := tls.X509KeyPair(
		clientSecret.Data[corev1.TLSCertKey],
		clientSecret.Data[corev1.TLSPrivateKeyKey],
	)
	if err != nil {
		contextLogger.Error(err, "Error while parsing client key and certificate for mTLS authentication",
			"secretName", clientSecret.Name)
		return ctrl.Result{}, err
	}

	serverCertificatePool := x509.NewCertPool()
	if ok := serverCertificatePool.AppendCertsFromPEM(serverSecret.Data[corev1.TLSCertKey]); !ok {
		secretLogger := contextLogger.WithValues("secretName", serverSecret.Name,
			"secretKey", corev1.TLSCertKey)
		// The certificate parsing failed, but unfortunately we are not aware of
		// the root cause.
		//
		// To emit a better log message, we individually execute the parsing
		// step and look at the real error.
		block, _ := pem.Decode(serverSecret.Data[corev1.TLSCertKey])
		if block == nil {
			err := fmt.Errorf("no valid PEM block found in server certificate from secret %q", serverSecret.Name)
			secretLogger.Error(err, "Error while parsing server certificate for mTLS authentication")
			return ctrl.Result{}, err
		}

		_, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			// If we don't manage to get the real error, we fall back to the
			// generic one.
			err = fmt.Errorf(
				"could not parse the server certificate from secret %q, please check the certificate format and validity",
				serverSecret.Name,
			)
		}

		secretLogger.Error(err, "Error while parsing server certificate for mTLS authentication")
		return ctrl.Result{}, err
	}

	pluginAddress := fmt.Sprintf("%s:%d", service.Name, pluginPort)

	// Use custom server name if provided, otherwise default to service name
	serverName := service.Annotations[utils.PluginServerNameAnnotationName]
	if len(serverName) == 0 {
		serverName = service.Name
	}

	err = r.Plugins.RegisterRemotePlugin(
		pluginName,
		pluginAddress,
		&tls.Config{
			ServerName: serverName,
			RootCAs:    serverCertificatePool,
			Certificates: []tls.Certificate{
				clientKeyPair,
			},
			MinVersion: tls.VersionTLS13,
		},
	)
	if err != nil {
		var errAlreadyAvailable *repository.ErrPluginAlreadyRegistered
		if errors.As(err, &errAlreadyAvailable) {
			// TODO(leonardoce): refresh plugin configuration
			contextLogger.Info("Plugin already registered")
			return ctrl.Result{}, nil
		}
		contextLogger.Error(err, "Error while registering plugin")
		return ctrl.Result{}, err
	}

	contextLogger.Info("Registered plugin")

	return ctrl.Result{}, nil
}

func (r *PluginReconciler) handleDeletion(
	ctx context.Context,
	service *corev1.Service,
	pluginName string,
) error {
	contextLogger := log.FromContext(ctx).WithValues("pluginName", pluginName)

	if controllerutil.ContainsFinalizer(service, utils.PluginFinalizerName) {
		contextLogger.Info("Removing plugin from pool due to service deletion")
		r.Plugins.ForgetPlugin(pluginName)

		contextLogger.Debug("Removing finalizer from plugin service")
		controllerutil.RemoveFinalizer(service, utils.PluginFinalizerName)
		if err := r.Update(ctx, service); err != nil {
			contextLogger.Error(err, "Error while removing finalizer from plugin service")
			return err
		}
	}

	return nil
}

func (r *PluginReconciler) getSecret(
	ctx context.Context,
	key client.ObjectKey,
) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, key, &secret); err != nil {
		return nil, err
	}

	return &secret, nil
}

func (r *PluginReconciler) mapSecretToPlugin(ctx context.Context, obj client.Object) []reconcile.Request {
	// We only consider the secrets that are installed in the
	// operator namespace because plugins need to be deployed
	// in the same namespace as the operator.
	if obj.GetNamespace() != r.OperatorNamespace {
		return nil
	}

	logger := log.FromContext(ctx)

	var services corev1.ServiceList
	if err := r.List(
		ctx,
		&services,
		client.HasLabels{utils.PluginNameLabelName},
		client.InNamespace(r.OperatorNamespace),
	); err != nil {
		logger.Error(
			err,
			"Error while listing CNPG-I services in the operator namespace",
		)
		return nil
	}

	var result []reconcile.Request
	for i := range services.Items {
		service := &services.Items[i]
		if isSecretUsedByPluginService(service, obj.GetName()) {
			result = append(result, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(service),
			})
		}
	}

	return result
}

// SetupWithManager adds this PluginReconciler to the passed controller manager
func (r *PluginReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrentReconciles int,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		For(&corev1.Service{}).
		Named("plugin").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToPlugin),
		).
		Complete(r)
}
