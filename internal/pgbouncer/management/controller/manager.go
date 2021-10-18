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

	"k8s.io/apimachinery/pkg/fields"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// PgBouncerReconciler can reconcile the status of the PostgreSQL cluster with
// the one of this PostgreSQL instance. Also the configuration in the
// ConfigMap is applied when needed
type PgBouncerReconciler struct {
	client      ctrl.WithWatch
	poolerWatch watch.Interface
	instance    PgBouncerInstanceInterface
	namespace   string
	poolerName  string
}

// NewPgBouncerReconciler creates a new pgbouncer reconciler
func NewPgBouncerReconciler(poolerName string, namespace string) (*PgBouncerReconciler, error) {
	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, err
	}

	return &PgBouncerReconciler{
		client:     client,
		instance:   NewPgBouncerInstance(),
		poolerName: poolerName,
		namespace:  namespace,
	}, nil
}

// Run runs the reconciliation loop for this resource
func (r *PgBouncerReconciler) Run(ctx context.Context) {
	for {
		// Retry with exponential back-off, unless it is a connection refused error
		err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
			log.Error(err, "Error calling Watch")
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
func (r *PgBouncerReconciler) watch(ctx context.Context) error {
	var err error

	r.poolerWatch, err = r.client.Watch(ctx, &apiv1.PoolerList{}, &ctrl.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.poolerName),
		Namespace:     r.namespace,
	})
	if err != nil {
		return fmt.Errorf("error watching pooler: %w", err)
	}
	defer r.Stop()

	for event := range r.poolerWatch.ResultChan() {
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
func (r *PgBouncerReconciler) Stop() {
	if r.poolerWatch != nil {
		r.poolerWatch.Stop()
	}
}

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *PgBouncerReconciler) GetClient() ctrl.Client {
	return r.client
}

// Reconcile is the main reconciliation loop for the pgbouncer instance
func (r *PgBouncerReconciler) Reconcile(ctx context.Context, event *watch.Event) error {
	contextLogger, _ := log.SetupLogger(ctx)
	contextLogger.Debug(
		"Reconciliation loop",
		"eventType", event.Type,
		"type", event.Object.GetObjectKind().GroupVersionKind())

	pooler, ok := event.Object.(*apiv1.Pooler)
	if !ok {
		return fmt.Errorf("error decoding pooler resource")
	}

	return r.reconcilePause(pooler)
}

func (r *PgBouncerReconciler) reconcilePause(pooler *apiv1.Pooler) error {
	isPaused := r.instance.Paused()
	shouldBePaused := pooler.Spec.PgBouncer.IsPaused()
	if shouldBePaused && !isPaused {
		if err := r.instance.Pause(); err != nil {
			return fmt.Errorf("while pausing instance: %w", err)
		}
	}
	if !shouldBePaused && isPaused {
		if err := r.instance.Resume(); err != nil {
			return fmt.Errorf("while resuming instance: %w", err)
		}
	}
	return nil
}
