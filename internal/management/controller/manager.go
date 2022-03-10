/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controller contains the function in PGK that reacts to events in
// the cluster.
package controller

import (
	"context"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/concurrency"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver/metricserver"
)

// InstanceReconciler can reconcile the status of the PostgreSQL cluster with
// the one of this PostgreSQL instance. Also the configuration in the
// ConfigMap is applied when needed
type InstanceReconciler struct {
	client   ctrl.Client
	instance *postgres.Instance

	secretVersions  map[string]string
	extensionStatus map[string]bool

	systemInitialization           *concurrency.Executed
	verifiedPrimaryPgDataCoherence atomic.Bool
	metricsServerExporter          *metricserver.Exporter
}

// NewInstanceReconciler creates a new instance reconciler
func NewInstanceReconciler(
	instance *postgres.Instance,
	client ctrl.Client,
	server *metricserver.MetricsServer,
) *InstanceReconciler {
	return &InstanceReconciler{
		instance:              instance,
		client:                client,
		secretVersions:        make(map[string]string),
		extensionStatus:       make(map[string]bool),
		systemInitialization:  concurrency.NewExecuted(),
		metricsServerExporter: server.GetExporter(),
	}
}

// GetInitialized returns the a condition that can be checked in order to
// be sure initialization has been done
func (r *InstanceReconciler) GetInitialized() *concurrency.Executed {
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
			Namespace: r.instance.Namespace,
			Name:      r.instance.ClusterName,
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}
