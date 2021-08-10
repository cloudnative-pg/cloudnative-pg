/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controller contains the function in PGK that reacts to events in
// the cluster.
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// InstanceReconciler can reconcile the status of the PostgreSQL cluster with
// the one of this PostgreSQL instance. Also the configuration in the
// ConfigMap is applied when needed
type InstanceReconciler struct {
	client          ctrl.Client
	dynamicClient   dynamic.Interface
	instance        *postgres.Instance
	log             logr.Logger
	watchCollection *WatchCollection

	secretVersions map[string]string
}

// NewInstanceReconciler creates a new instance reconciler
func NewInstanceReconciler(instance *postgres.Instance) (*InstanceReconciler, error) {
	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, err
	}

	// Unfortunately we need a dynamic client to watch over Clusters, because
	// `controller-runtime` 0.8.0 don't have that feature. `0.9.0` will have
	// a generic interface over watches, so let's wait for it.

	dynamicClient, err := management.NewDynamicClient()
	if err != nil {
		return nil, err
	}

	return &InstanceReconciler{
		instance:       instance,
		log:            log.Log,
		client:         client,
		dynamicClient:  dynamicClient,
		secretVersions: make(map[string]string),
	}, nil
}

// Run runs the reconciliation loop for this resource
func (r *InstanceReconciler) Run(ctx context.Context) {
	for {
		// Retry with exponential back-off, unless it is a connection refused error
		err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
			r.log.Error(err, "Error calling Watch", "cluster", r.instance.ClusterName)
			return !utilnet.IsConnectionRefused(err)
		}, func() error {
			return r.watch(ctx)
		})
		if err != nil {
			// If this is "connection refused" error, it means that apiserver is probably not responsive.
			// If that's the case wait and resend watch request.
			time.Sleep(time.Second)
		}
	}
}

// watch contains the main reconciler loop
func (r *InstanceReconciler) watch(ctx context.Context) error {
	var err error

	// 1. Prepare the set of watches for objects we are interested in
	//    keeping synchronized with the instance status

	// This is an example of how to watch a certain object
	// https://github.com/kubernetes/kubernetes/issues/43299
	clusterWatch, err := r.dynamicClient.
		Resource(apiv1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching cluster: %w", err)
	}

	r.watchCollection = NewWatchCollection(
		clusterWatch,
	)
	defer r.Stop()

	for event := range r.watchCollection.ResultChan() {
		receivedEvent := event
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Reconcile(ctx, &receivedEvent)
		})
		if err != nil {
			r.log.Error(err, "Reconciliation error")
		}
	}

	return nil
}

// Stop stops the controller
func (r *InstanceReconciler) Stop() {
	if r.watchCollection != nil {
		r.watchCollection.Stop()
	}
}

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *InstanceReconciler) GetClient() ctrl.Client {
	return r.client
}
