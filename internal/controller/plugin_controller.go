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

// Package controller contains the controller of the CRD
package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PluginReconciler reconciles CNPG-i plugins
type PluginReconciler struct {
	client.Client

	Scheme  *runtime.Scheme
	Plugins repository.Interface
}

// NewPluginReconciler creates a new PluginReconciler initializing it
func NewPluginReconciler(mgr manager.Manager, plugins repository.Interface) *PluginReconciler {
	return &PluginReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Plugins: plugins,
	}
}

// Reconcile is the reconciler loop
func (r *PluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Plugin reconciliation loop start")
	defer func() {
		contextLogger.Debug("Plugin reconciliation loop end")
	}()

	var service corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		// TODO(leonardoce): use a finalizer to detect when a plugin service
		// is removed, and remove the corresponding plugin from the pool

		// This also happens when you delete a resource in k8s
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		// This is a real error, maybe the RBAC configuration is wrong?
		return ctrl.Result{}, fmt.Errorf("cannot get the resource: %w", err)
	}

	// Process label and annotations
	pluginName := service.Labels[utils.PluginNameLabelName]
	if len(pluginName) == 0 {
		contextLogger.Info("Detected service whose plugin name label is empty, skipping")
		return ctrl.Result{}, nil
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
	contextLogger := log.FromContext(ctx).WithValues("pluginName", pluginName)

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
		contextLogger.Error(err, "Error while parsing client key and certificate for mTLS authentication")
		return ctrl.Result{}, err
	}

	serverCertificatePool := x509.NewCertPool()
	if ok := serverCertificatePool.AppendCertsFromPEM(serverSecret.Data[corev1.TLSCertKey]); !ok {
		// Unfortunately, by doing that, we loose the certificate parsing error
		// and we don't know if the problem lies in the PEM block or in the DER content
		err := fmt.Errorf("parsing error")
		contextLogger.Error(err, "Error while parsing server certificate for mTLS authentication")
		return ctrl.Result{}, err
	}

	pluginAddress := fmt.Sprintf("%s:%d", service.Name, pluginPort)

	err = r.Plugins.RegisterRemotePlugin(
		pluginName,
		pluginAddress,
		&tls.Config{
			ServerName: service.Name,
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

// SetupWithManager adds this PluginReconciler to the passed controller manager
func (r *PluginReconciler) SetupWithManager(mgr ctrl.Manager, operatorNamespace string) error {
	pluginServicesPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isPluginService(e.Object, operatorNamespace)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isPluginService(e.Object, operatorNamespace)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isPluginService(e.Object, operatorNamespace)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isPluginService(e.ObjectNew, operatorNamespace)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Named("plugin").
		WithEventFilter(pluginServicesPredicate).
		Complete(r)
}
