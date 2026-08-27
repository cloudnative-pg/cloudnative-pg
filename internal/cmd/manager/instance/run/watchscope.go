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

package run

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/upgrade"
)

// databaseWatchScopeReconciler restarts the instance manager when the namespaces
// the Database objects are watched in no longer match the ones this process
// started with.
//
// The scope of the cache of a controller-runtime manager is fixed when the
// manager is built, so enabling the cross-namespace Database support on a
// running cluster cannot widen the watch of an already started instance manager.
// Instead of leaving the cross-namespace Database objects unreconciled until the
// pod is recreated, this reconciler replaces the instance manager process with a
// new one, which reads the current definition of the cluster while starting.
// PostgreSQL keeps running across the restart.
type databaseWatchScopeReconciler struct {
	client          client.Client
	instance        *postgres.Instance
	crossNamespace  bool
	cancelFunc      context.CancelFunc
	exitedCondition concurrency.MultipleExecuted

	// restart replaces this process, and never returns when it succeeds. It is
	// a field to keep the reconciler testable.
	restart func(context.CancelFunc, concurrency.MultipleExecuted) error
}

// newDatabaseWatchScopeReconciler builds a databaseWatchScopeReconciler for the
// passed instance, whose cache watches Database objects cluster-wide only when
// crossNamespace is true
func newDatabaseWatchScopeReconciler(
	cli client.Client,
	instance *postgres.Instance,
	crossNamespace bool,
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
) *databaseWatchScopeReconciler {
	return &databaseWatchScopeReconciler{
		client:          cli,
		instance:        instance,
		crossNamespace:  crossNamespace,
		cancelFunc:      cancelFunc,
		exitedCondition: exitedCondition,
		restart:         upgrade.RestartInstanceManager,
	}
}

// Reconcile restarts the instance manager when the cross-namespace Database
// support of the cluster does not match the one of this process
func (r *databaseWatchScopeReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var cluster apiv1.Cluster
	if err := r.client.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if cluster.Spec.EnableCrossNamespaceDatabases == r.crossNamespace {
		return ctrl.Result{}, nil
	}

	// Signal to the PostgreSQL lifecycle manager that the coming cancellation of
	// the context must not stop PostgreSQL. When an online upgrade of the
	// instance manager is already running, it is going to restart this process
	// anyway, so there is nothing left to do here.
	if !r.instance.InstanceManagerIsUpgrading.CompareAndSwap(false, true) {
		contextLogger.Info(
			"Cross-namespace databases support changed while the instance manager is upgrading, " +
				"the upgrade will apply the new configuration")
		return ctrl.Result{}, nil
	}

	contextLogger.Info(
		"Cross-namespace databases support changed, restarting the instance manager to apply it",
		"enableCrossNamespaceDatabases", cluster.Spec.EnableCrossNamespaceDatabases)

	// This does not return when it succeeds, as the process is replaced
	if err := r.restart(r.cancelFunc, r.exitedCondition); err != nil {
		r.instance.InstanceManagerIsUpgrading.Store(false)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler inside the controller manager
func (r *databaseWatchScopeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Named("database-watch-scope").
		Complete(r)
}
