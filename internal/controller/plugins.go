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
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
)

// preReconcilePluginHooks ensures we call the pre-reconcile plugin hooks
func preReconcilePluginHooks(
	ctx context.Context,
	cluster *apiv1.Cluster,
	object client.Object,
) cnpgiClient.ReconcilerHookResult {
	pluginClient := cnpgiClient.GetPluginClientFromContext(ctx)
	return pluginClient.PreReconcile(ctx, cluster, object)
}

// postReconcilePluginHooks ensures we call the post-reconcile plugin hooks
func postReconcilePluginHooks(
	ctx context.Context,
	cluster *apiv1.Cluster,
	object client.Object,
) cnpgiClient.ReconcilerHookResult {
	pluginClient := cnpgiClient.GetPluginClientFromContext(ctx)
	return pluginClient.PostReconcile(ctx, cluster, object)
}

func setStatusPluginHook(
	ctx context.Context,
	cli client.Client,
	pluginClient cnpgiClient.Client,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx).WithName("set_status_plugin_hook")

	origCluster := cluster.DeepCopy()
	statuses, err := pluginClient.SetStatusInCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while calling SetStatusInCluster: %w", err)
	}
	if len(statuses) == 0 {
		return ctrl.Result{}, nil
	}
	for idx := range cluster.Status.PluginStatus {
		plugin := &cluster.Status.PluginStatus[idx]
		val, ok := statuses[plugin.Name]
		if !ok {
			continue
		}
		plugin.Status = val
	}

	contextLogger.Info("patching cluster status with the updated plugin statuses")
	contextLogger.Debug("diff detected",
		"before", origCluster.Status.PluginStatus,
		"after", cluster.Status.PluginStatus,
	)

	if err := cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}
