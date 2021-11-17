/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controller contains the function in PGK that reacts to events in
// the cluster.
package controller

import (
	"context"
	"time"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
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
	client       ctrl.WithWatch
	instance     *postgres.Instance
	clusterWatch watch.Interface

	secretVersions  map[string]string
	extensionStatus map[string]bool
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

	return &InstanceReconciler{
		instance:        instance,
		client:          client,
		secretVersions:  make(map[string]string),
		extensionStatus: make(map[string]bool),
	}, nil
}

// Run runs the reconciliation loop for this resource
func (r *InstanceReconciler) Run(ctx context.Context) {
	for {
		errorIsRetriable := func(err error) bool {
			log.Warning("Error watching cluster resource",
				"cluster", r.instance.ClusterName,
				"err", err,
				"reason", apierrs.ReasonForError(err))

			// Better to wait a bit, probably the API server is not responsive
			if utilnet.IsConnectionRefused(err) {
				return false
			}

			// The cluster has been deleted, no need to retry
			if apierrs.IsNotFound(err) {
				return false
			}

			// We are not presenting any right credential for some reason,
			// e.g. the service account was already deleted.
			// Therefore, no need to retry as we would continue failing.
			if apierrs.IsUnauthorized(err) {
				return false
			}

			return true
		}

		// Retry with exponential back-off, unless it's a retryable error according to the definition above
		err := retry.OnError(retry.DefaultBackoff, errorIsRetriable, func() error {
			return r.watch(ctx)
		})

		switch {
		case err == nil:
			// nothing to do here beside trying again

		case apierrs.IsNotFound(err):
			// The cluster we are watching doesn't exist anymore.
			// There is no need to retry
			return

		default:
			// Let's wait a bit before retrying it again
			log.Error(err, "Waiting one second before retrying watching the cluster")
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
	r.clusterWatch, err = r.client.Watch(ctx, &apiv1.ClusterList{}, &ctrl.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName),
		Namespace:     r.instance.Namespace,
	})
	if err != nil {
		// Make sure to not decorate the error, otherwise the caller
		// will fail identifying the cause.
		return err
	}
	defer r.Stop()

	for event := range r.clusterWatch.ResultChan() {
		receivedEvent := event
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Reconcile(ctx, &receivedEvent)
		})
		if err != nil {
			log.Error(err, "Reconciliation error")
		}
	}

	return nil
}

// Stop stops the controller
func (r *InstanceReconciler) Stop() {
	if r.clusterWatch != nil {
		r.clusterWatch.Stop()
	}
}

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *InstanceReconciler) GetClient() ctrl.Client {
	return r.client
}

// Instance get the PostgreSQL instance that this reconciler is working on
func (r *InstanceReconciler) Instance() *postgres.Instance {
	return r.instance
}
