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

package controller

import (
	"context"

	"github.com/cloudnative-pg/machinery/pkg/log"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// primaryLeasePredicate enqueues the parent Cluster only when the owned Lease
// is deleted. Renew/holder updates happen every few seconds and would otherwise
// trigger a reconcile storm.
var primaryLeasePredicate = predicate.Funcs{
	CreateFunc:  func(event.CreateEvent) bool { return false },
	UpdateFunc:  func(event.UpdateEvent) bool { return false },
	DeleteFunc:  func(event.DeleteEvent) bool { return true },
	GenericFunc: func(event.GenericEvent) bool { return false },
}

// reconcilePrimaryLease ensures a Lease object named after the cluster exists and is owned by it.
// The instance manager uses this lease as a primary-election mutex.
func (r *ClusterReconciler) reconcilePrimaryLease(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	lease := coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
	}
	cluster.SetInheritedDataAndOwnership(&lease.ObjectMeta)

	switch err := r.Create(ctx, &lease); {
	case apierrs.IsAlreadyExists(err):
		// Steady state: the lease already exists, nothing to do. This is the
		// common path on every reconcile, so it is intentionally not logged.
		return nil
	case err != nil:
		return err
	}

	// The Create succeeded, meaning the lease was absent: either the cluster was
	// just bootstrapped, or the lease was deleted and this reconcile (triggered by
	// the deletion watch) is recreating it.
	contextLogger.Info("Created primary lease", "leaseName", lease.Name)
	return nil
}
