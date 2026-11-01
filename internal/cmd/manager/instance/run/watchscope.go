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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/upgrade"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// clusterWideAccessRetryDelay is how long to wait before checking again whether
// the managed resources can be watched in every namespace. The operator grants
// that access right after the cross-namespace resources support is enabled, so
// a short delay is enough.
const clusterWideAccessRetryDelay = 5 * time.Second

// restartHeldBackRetryDelay is how long to wait before checking again whether
// the instance manager can be restarted, after the restart was held back. The
// end of a switchover or of a rolling update changes the cluster, which
// triggers a new check by itself, but the end of a backup does not.
const restartHeldBackRetryDelay = 30 * time.Second

// clusterWideResources are the managed resources that the cache of the
// instance manager watches in every namespace when the cross-namespace
// resources support is enabled
var clusterWideResources = []string{"databases", "databaseroles", "publications", "subscriptions"}

// clusterWideVerbs are the verbs the informers of the cache need
var clusterWideVerbs = []string{"list", "watch"}

// watchScopeReconciler restarts the instance manager when the namespaces
// the managed resources are watched in no longer match the ones this process
// started with.
//
// The scope of the cache of a controller-runtime manager is fixed when the
// manager is built, so enabling the cross-namespace resources support on a
// running cluster cannot widen the watch of an already started instance manager.
// Instead of leaving the cross-namespace resources unreconciled until the
// pod is recreated, this reconciler replaces the instance manager process with a
// new one, which reads the current definition of the cluster while starting.
// PostgreSQL keeps running across the restart.
//
// The instance manager is not available while it restarts, so the restart is
// held back while it could interrupt its work: while the cluster is not
// healthy, as during a switchover, a failover, a rolling update or an online
// upgrade, and while a backup is taken from this instance, which would fail.
type watchScopeReconciler struct {
	client          client.Client
	instance        *postgres.Instance
	crossNamespace  bool
	cancelFunc      context.CancelFunc
	exitedCondition concurrency.MultipleExecuted

	// restart replaces this process, and never returns when it succeeds. It is
	// a field to keep the reconciler testable.
	restart func(context.CancelFunc, concurrency.MultipleExecuted) error

	// checkAccess tells whether this instance manager can already list and watch
	// the managed resources in every namespace
	checkAccess clusterWideAccessChecker
}

// clusterWideAccessChecker reports whether the managed resources can be listed
// and watched in every namespace. When they cannot, it returns a description of
// the missing permission.
type clusterWideAccessChecker func(ctx context.Context) (allowed bool, reason string, err error)

// newClusterWideAccessChecker builds a clusterWideAccessChecker asking the
// API server, through SelfSubjectAccessReviews, whether this instance manager can
// list and watch the managed resources in every namespace.
func newClusterWideAccessChecker(kubeClient kubernetes.Interface) clusterWideAccessChecker {
	return func(ctx context.Context) (bool, string, error) {
		return utils.CheckClusterWideAccess(
			ctx, kubeClient, apiv1.SchemeGroupVersion.Group, clusterWideResources, clusterWideVerbs)
	}
}

// newWatchScopeReconciler builds a watchScopeReconciler for the
// passed instance, whose cache watches Database objects cluster-wide only when
// crossNamespace is true
func newWatchScopeReconciler(
	cli client.Client,
	kubeClient kubernetes.Interface,
	instance *postgres.Instance,
	crossNamespace bool,
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
) *watchScopeReconciler {
	return &watchScopeReconciler{
		client:          cli,
		instance:        instance,
		crossNamespace:  crossNamespace,
		cancelFunc:      cancelFunc,
		exitedCondition: exitedCondition,
		restart:         upgrade.RestartInstanceManager,
		checkAccess:     newClusterWideAccessChecker(kubeClient),
	}
}

// Reconcile restarts the instance manager when the cross-namespace resources
// support of the cluster does not match the one of this process
func (r *watchScopeReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var cluster apiv1.Cluster
	if err := r.client.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if cluster.Spec.EnableCrossNamespaceResources == r.crossNamespace {
		return ctrl.Result{}, nil
	}

	reason, err := r.restartHeldBackReason(ctx, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if reason != "" {
		contextLogger.Info(
			"Cross-namespace resources support changed, holding back the restart of the instance manager",
			"reason", reason,
			"retryAfter", restartHeldBackRetryDelay)
		return ctrl.Result{RequeueAfter: restartHeldBackRetryDelay}, nil
	}

	// A new process would start its informers in every namespace right away.
	// If the operator did not grant that access yet, they would be refused,
	// the cache would never sync and the probes would stop answering, making
	// the kubelet restart PostgreSQL. Wait until the access is usable.
	// Narrowing the scope needs no new permission.
	if cluster.Spec.EnableCrossNamespaceResources {
		allowed, reason, err := r.checkAccess(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !allowed {
			contextLogger.Info(
				"Cross-namespace resources support enabled, waiting for the cluster-wide access "+
					"before restarting the instance manager",
				"reason", reason,
				"retryAfter", clusterWideAccessRetryDelay)
			return ctrl.Result{RequeueAfter: clusterWideAccessRetryDelay}, nil
		}
	}

	// Signal to the PostgreSQL lifecycle manager that the coming cancellation of
	// the context must not stop PostgreSQL. When an online upgrade of the
	// instance manager is already running, it is going to restart this process
	// anyway, so there is nothing left to do here.
	if !r.instance.InstanceManagerIsUpgrading.CompareAndSwap(false, true) {
		contextLogger.Info(
			"Cross-namespace resources support changed while the instance manager is upgrading, " +
				"the upgrade will apply the new configuration")
		return ctrl.Result{}, nil
	}

	contextLogger.Info(
		"Cross-namespace resources support changed, restarting the instance manager to apply it",
		"enableCrossNamespaceResources", cluster.Spec.EnableCrossNamespaceResources)

	// This does not return when it succeeds, as the process is replaced
	if err := r.restart(r.cancelFunc, r.exitedCondition); err != nil {
		r.instance.InstanceManagerIsUpgrading.Store(false)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// restartHeldBackReason returns why the instance manager must not be restarted
// yet, or an empty string when it can be
func (r *watchScopeReconciler) restartHeldBackReason(ctx context.Context, cluster *apiv1.Cluster) (string, error) {
	if cluster.Status.Phase != apiv1.PhaseHealthy {
		return fmt.Sprintf("the cluster is not healthy (phase: %q)", cluster.Status.Phase), nil
	}

	if cluster.Status.TargetPrimary != cluster.Status.CurrentPrimary {
		return "a switchover or a failover is in progress", nil
	}

	var backups apiv1.BackupList
	if err := r.client.List(ctx, &backups, client.InNamespace(cluster.Namespace)); err != nil {
		return "", fmt.Errorf("while listing the backups: %w", err)
	}
	for i := range backups.Items {
		backup := &backups.Items[i]
		if backup.Spec.Cluster.Name == cluster.Name &&
			backup.Status.IsExecuting() &&
			backup.Status.InstanceID != nil &&
			backup.Status.InstanceID.PodName == r.instance.GetPodName() {
			return fmt.Sprintf("backup %q is running on this instance", backup.Name), nil
		}
	}

	return "", nil
}

// SetupWithManager registers the reconciler inside the controller manager
func (r *watchScopeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Named("cross-namespace-watch-scope").
		Complete(r)
}
