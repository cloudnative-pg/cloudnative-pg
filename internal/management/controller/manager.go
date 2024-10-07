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

// Package controller contains the functions in PostgreSQL instance manager
// that reacts to changes to the Cluster resource.
package controller

import (
	"context"
	"fmt"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"
)

// InstanceReconciler reconciles the status of the Cluster resource with
// the one of this PostgreSQL instance. Also, the configuration in the
// ConfigMap is applied when needed
type InstanceReconciler struct {
	client   ctrl.Client
	instance *postgres.Instance

	secretVersions  map[string]string
	extensionStatus map[string]bool

	systemInitialization  *concurrency.Executed
	firstReconcileDone    atomic.Bool
	metricsServerExporter *metricserver.Exporter
}

// NewInstanceReconciler creates a new instance reconciler
func NewInstanceReconciler(
	instance *postgres.Instance,
	client ctrl.Client,
	metricsExporter *metricserver.Exporter,
) *InstanceReconciler {
	return &InstanceReconciler{
		instance:              instance,
		client:                client,
		secretVersions:        make(map[string]string),
		extensionStatus:       make(map[string]bool),
		systemInitialization:  concurrency.NewExecuted(),
		metricsServerExporter: metricsExporter,
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
	var cluster apiv1.Cluster
	err := r.GetClient().Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.GetNamespaceName(),
			Name:      r.instance.GetClusterName(),
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
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
