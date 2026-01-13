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

// Package controller contains the functions in PostgreSQL instance manager
// that reacts to changes to the Cluster resource.
package controller

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/webhook/guard"
	webhookv1 "github.com/cloudnative-pg/cloudnative-pg/internal/webhook/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
	instancecertificate "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/certificate"
)

// InstanceReconciler reconciles the status of the Cluster resource with
// the one of this PostgreSQL instance. Also, the configuration in the
// ConfigMap is applied when needed
type InstanceReconciler struct {
	client        ctrl.Client
	instance      *postgres.Instance
	runningImages *stringset.Data

	secretVersions  map[string]string
	extensionStatus map[string]bool

	systemInitialization  *concurrency.Executed
	firstReconcileDone    atomic.Bool
	metricsServerExporter *metricserver.Exporter

	certificateReconciler *instancecertificate.Reconciler
	pluginRepository      repository.Interface
	admission             *guard.Admission
}

// NewInstanceReconciler creates a new instance reconciler
func NewInstanceReconciler(
	instance *postgres.Instance,
	client ctrl.Client,
	metricsExporter *metricserver.Exporter,
	pluginRepository repository.Interface,
) *InstanceReconciler {
	return &InstanceReconciler{
		instance:              instance,
		client:                client,
		secretVersions:        make(map[string]string),
		extensionStatus:       make(map[string]bool),
		systemInitialization:  concurrency.NewExecuted(),
		metricsServerExporter: metricsExporter,
		certificateReconciler: instancecertificate.NewReconciler(client, instance),
		pluginRepository:      pluginRepository,
		admission: &guard.Admission{
			Defaulter: &webhookv1.ClusterCustomDefaulter{},
			Validator: &webhookv1.ClusterCustomValidator{},
		},
	}
}

// GetExecutedCondition returns the condition that can be checked in order to
// be sure initialization has been done
func (r *InstanceReconciler) GetExecutedCondition() *concurrency.Executed {
	return r.systemInitialization
}

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *InstanceReconciler) GetClient() ctrl.Client {
	return r.client
}

// Instance get the PostgreSQL instance that this reconciler is working on
func (r *InstanceReconciler) Instance() *postgres.Instance {
	return r.instance
}

// GetCluster gets the managed cluster through the client
func (r *InstanceReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	return getClusterFromInstance(ctx, r.client, r.instance)
}

// GetSecret will get a named secret in the instance namespace
func (r *InstanceReconciler) GetSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	var secret corev1.Secret
	err := r.GetClient().Get(ctx,
		types.NamespacedName{
			Name:      name,
			Namespace: r.instance.GetNamespaceName(),
		}, &secret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret: %w", err)
	}
	return &secret, nil
}
