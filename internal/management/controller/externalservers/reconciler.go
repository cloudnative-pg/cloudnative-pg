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

package externalservers

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
)

// Reconcile is the main reconciliation loop for the instance
func (r *Reconciler) Reconcile(
	ctx context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("external_servers_reconciler")
	// if the context has already been cancelled,
	// trying to synchronize would just lead to misleading errors being reported
	if err := ctx.Err(); err != nil {
		contextLogger.Warning("Context cancelled, will not start externalservers reconcile", "err", err)
		return reconcile.Result{}, nil
	}

	// Fetch the Cluster from the cache
	cluster, err := r.getCluster(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster has been deleted.
			// We just need to wait for this instance manager to be terminated
			contextLogger.Debug("Could not find Cluster")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not fetch Cluster: %w", err)
	}

	contextLogger.Debug("starting up the external servers reconciler")

	// For each external server, we ensure we download the credentials
	for i := range cluster.Spec.ExternalClusters {
		r.synchronize(
			ctx,
			cluster.Namespace,
			&cluster.Spec.ExternalClusters[i])
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) synchronize(
	ctx context.Context,
	namespace string,
	server *apiv1.ExternalCluster,
) {
	contextLogger := log.FromContext(ctx).WithValues("serverName", server.Name)

	connectionString, err := external.ConfigureConnectionToServer(ctx, r.client, namespace, server)
	if err != nil {
		contextLogger.Info("Cannot synchronize external server connection parameters", "err", err)
	} else {
		contextLogger.Debug("External server connection string", "connectionString", connectionString)
	}
}
