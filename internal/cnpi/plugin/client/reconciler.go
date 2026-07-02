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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/cloudnative-pg/machinery/pkg/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
)

const cnpgOperatorKey = "cnpg-operator"

// newContinueResult returns a result instructing the reconciliation loop
// to continue its operation
func newContinueResult(identifier string) ReconcilerHookResult {
	return ReconcilerHookResult{Identifier: identifier}
}

// newTerminateResult returns a result instructing the reconciliation loop to stop
// reconciliation
func newTerminateResult(identifier string) ReconcilerHookResult {
	return ReconcilerHookResult{StopReconciliation: true, Identifier: identifier}
}

// newReconcilerRequeueResult creates a new result instructing
// a reconciler to schedule a loop in the passed time frame
func newReconcilerRequeueResult(identifier string, after int64) ReconcilerHookResult {
	return ReconcilerHookResult{
		Err:                nil,
		StopReconciliation: true,
		Result:             ctrl.Result{Requeue: true, RequeueAfter: time.Second * time.Duration(after)},
		Identifier:         identifier,
	}
}

// newReconcilerErrorResult creates a new result from an error
func newReconcilerErrorResult(identifier string, err error) ReconcilerHookResult {
	return ReconcilerHookResult{Err: wrapAsPluginErrorIfNeeded(err), StopReconciliation: true, Identifier: identifier}
}

func (data *data) PreReconcile(ctx context.Context, cluster client.Object, object client.Object) ReconcilerHookResult {
	return reconcilerHook(
		ctx,
		cluster,
		object,
		data.plugins,
		func(
			ctx context.Context,
			plugin reconciler.ReconcilerHooksClient,
			request *reconciler.ReconcilerHooksRequest,
		) (*reconciler.ReconcilerHooksResult, error) {
			return plugin.Pre(ctx, request)
		},
	)
}

func (data *data) PostReconcile(ctx context.Context, cluster client.Object, object client.Object) ReconcilerHookResult {
	return reconcilerHook(
		ctx,
		cluster,
		object,
		data.plugins,
		func(
			ctx context.Context,
			plugin reconciler.ReconcilerHooksClient,
			request *reconciler.ReconcilerHooksRequest,
		) (*reconciler.ReconcilerHooksResult, error) {
			return plugin.Post(ctx, request)
		},
	)
}

type reconcilerHookFunc func(
	ctx context.Context,
	plugin reconciler.ReconcilerHooksClient,
	request *reconciler.ReconcilerHooksRequest,
) (*reconciler.ReconcilerHooksResult, error)

func reconcilerHook(
	ctx context.Context,
	cluster client.Object,
	object client.Object,
	plugins []connection.Interface,
	executeRequest reconcilerHookFunc,
) ReconcilerHookResult {
	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return newReconcilerErrorResult(
			cnpgOperatorKey,
			fmt.Errorf("while serializing %s %s/%s to JSON: %w",
				cluster.GetObjectKind().GroupVersionKind().Kind,
				cluster.GetNamespace(), cluster.GetName(),
				err,
			),
		)
	}

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return newReconcilerErrorResult(
			cnpgOperatorKey,
			fmt.Errorf(
				"while serializing %s %s/%s to JSON: %w",
				cluster.GetObjectKind().GroupVersionKind().Kind,
				cluster.GetNamespace(), cluster.GetName(),
				err,
			),
		)
	}

	request := &reconciler.ReconcilerHooksRequest{
		ClusterDefinition:  serializedCluster,
		ResourceDefinition: serializedObject,
	}

	var kind reconciler.ReconcilerHooksCapability_Kind
	switch object.GetObjectKind().GroupVersionKind().Kind {
	case "Cluster":
		kind = reconciler.ReconcilerHooksCapability_KIND_CLUSTER
	case "Backup":
		kind = reconciler.ReconcilerHooksCapability_KIND_BACKUP
	default:
		contextLogger.Info(
			"Skipping reconciler hooks for unknown group",
			"objectGvk", object.GetObjectKind())
		return newContinueResult(cnpgOperatorKey)
	}

	for idx := range plugins {
		plugin := plugins[idx]

		if !slices.Contains(plugin.ReconcilerCapabilities(), kind) {
			continue
		}

		result, err := executeRequest(ctx, plugin.ReconcilerHooksClient(), request)
		if err != nil {
			return newReconcilerErrorResult(plugin.Name(), err)
		}

		switch result.Behavior {
		case reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE:
			return newTerminateResult(plugin.Name())

		case reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE:
			return newReconcilerRequeueResult(plugin.Name(), result.GetRequeueAfter())

		case reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE:
			// Continue to next plugin
		}
	}

	return newContinueResult(cnpgOperatorKey)
}
