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

package controllers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpiClient "github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/client"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// preReconcilePluginHooks ensures we call the pre-reconcile plugin hooks
func preReconcilePluginHooks(
	ctx context.Context,
	cluster *apiv1.Cluster,
	object client.Object,
) cnpiClient.ReconcilerHookResult {
	contextLogger := log.FromContext(ctx)

	// Load the plugins
	pluginClient, err := cluster.LoadPluginClient(ctx)
	if err != nil {
		contextLogger.Error(err, "Error loading plugins, retrying")
		return cnpiClient.ReconcilerHookResult{
			Err:                err,
			StopReconciliation: true,
		}
	}
	defer func() {
		pluginClient.Close(ctx)
	}()

	return pluginClient.PreReconcile(ctx, cluster, object)
}

// postReconcilePluginHooks ensures we call the post-reconcile plugin hooks
func postReconcilePluginHooks(
	ctx context.Context,
	cluster *apiv1.Cluster,
	object client.Object,
) cnpiClient.ReconcilerHookResult {
	contextLogger := log.FromContext(ctx)

	// Load the plugins
	pluginClient, err := cluster.LoadPluginClient(ctx)
	if err != nil {
		contextLogger.Error(err, "Error loading plugins, retrying")
		return cnpiClient.ReconcilerHookResult{
			Err:                err,
			StopReconciliation: true,
		}
	}
	defer func() {
		pluginClient.Close(ctx)
	}()

	return pluginClient.PostReconcile(ctx, cluster, object)
}
