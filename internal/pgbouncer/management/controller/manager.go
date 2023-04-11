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

// Package controller contains the functions in pgbouncer instance manager
// that reacts to changes in the Pooler resource.
package controller

import (
	"context"
	"fmt"
	"time"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
)

// PgBouncerReconciler reconciles the status of the Pooler resource with
// the one of this pgbouncer instance
type PgBouncerReconciler struct {
	client               ctrl.WithWatch
	poolerWatch          watch.Interface
	instance             PgBouncerInstanceInterface
	poolerNamespacedName types.NamespacedName
}

// NewPgBouncerReconciler creates a new pgbouncer reconciler
func NewPgBouncerReconciler(poolerNamespacedName types.NamespacedName) (*PgBouncerReconciler, error) {
	client, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, err
	}

	return &PgBouncerReconciler{
		client:               client,
		instance:             NewPgBouncerInstance(),
		poolerNamespacedName: poolerNamespacedName,
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
		FieldSelector: fields.OneTermEqualSelector("metadata.name", r.poolerNamespacedName.Name),
		Namespace:     r.poolerNamespacedName.Namespace,
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

	err := r.synchronizeConfig(ctx, pooler)
	if err != nil {
		return fmt.Errorf("while reconciling configuration: %w", err)
	}

	return r.synchronizePause(pooler)
}

// synchronizePause ensure that the pause flag inside the Pooler
// specification matches the PgBouncer status
func (r *PgBouncerReconciler) synchronizePause(pooler *apiv1.Pooler) error {
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

// synchronizeConfig ensure that the configuration derived from
// the pooler specification matches the one loaded in PgBouncer
func (r *PgBouncerReconciler) synchronizeConfig(ctx context.Context, pooler *apiv1.Pooler) error {
	var (
		configurationChanged bool
		err                  error
	)

	if configurationChanged, err = r.writePgBouncerConfig(ctx, pooler); err != nil {
		return fmt.Errorf("while writing PgBouncer configuration: %w", err)
	}

	if !configurationChanged {
		return nil
	}

	if err = r.instance.Reload(); err != nil {
		return fmt.Errorf("while reloading configuration due to change: %w", err)
	}

	return nil
}

// writePgBouncerConfig writes the PgBouncer configuration files given the Pooler
// specification, returning a boolean flag indicating if the configuration has
// changed or not
func (r *PgBouncerReconciler) writePgBouncerConfig(ctx context.Context, pooler *apiv1.Pooler) (bool, error) {
	var (
		secrets     *config.Secrets
		configFiles config.ConfigurationFiles

		err error
	)

	// If this is the first reconciliation loop the API server may
	// not have applied the RBAC rules created by the controller.
	// In this case we still don't have the permissions to read
	// the secrets we require.
	// This is why we are retrying the loading of the secrets.
	if err := retry.OnError(retry.DefaultBackoff, apierrs.IsForbidden, func() error {
		secrets, err = getSecrets(ctx, r.GetClient(), pooler)
		return err
	}); err != nil {
		return false, fmt.Errorf("while reading secrets: %w", err)
	}

	if configFiles, err = config.BuildConfigurationFiles(pooler, secrets); err != nil {
		return false, fmt.Errorf("while generating pgbouncer configuration: %w", err)
	}

	return refreshConfigurationFiles(configFiles)
}

// Init ensures that all PgBouncer requirement are met.
//
// In detail:
// 1. create the pgbouncer configuration and the required secrets
// 2. ensure that every needed folder is existent
func (r *PgBouncerReconciler) Init(ctx context.Context) error {
	var pooler apiv1.Pooler

	// Get the pooler from the API Server
	if err := r.GetClient().Get(ctx, r.poolerNamespacedName, &pooler); err != nil {
		return fmt.Errorf("while getting pooler for the first time: %w", err)
	}

	// Write the startup configuration for PgBouncer
	if _, err := r.writePgBouncerConfig(ctx, &pooler); err != nil {
		return err
	}

	// Ensure we have the directory to store the controlling socket
	if err := fileutils.EnsureDirectoryExists(config.PgBouncerSocketDir); err != nil {
		log.Error(err, "while checking socket directory existed", "dir", config.PgBouncerSocketDir)
		return err
	}

	return nil
}
