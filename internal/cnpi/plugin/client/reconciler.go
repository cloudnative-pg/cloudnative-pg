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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

func (data *data) PreReconcile(ctx context.Context, cluster client.Object, object client.Object) (ctrl.Result, error) {
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

func (data *data) PostReconcile(ctx context.Context, cluster client.Object, object client.Object) (ctrl.Result, error) {
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
	plugins []pluginData,
	executeRequest reconcilerHookFunc,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	request := &reconciler.ReconcilerHooksRequest{
		ClusterDefinition:  serializedCluster,
		ResourceDefinition: serializedObject,
	}

	var kind reconciler.ReconcilerHooksCapability_Kind
	switch cluster.GetObjectKind().GroupVersionKind().Kind {
	case "Cluster":
		kind = reconciler.ReconcilerHooksCapability_KIND_CLUSTER
	case "Backup":
		kind = reconciler.ReconcilerHooksCapability_KIND_BACKUP
	default:
		contextLogger.Info(
			"Skipping reconciler hooks for unknown group",
			"objectGvk", object.GetObjectKind())
		return ctrl.Result{}, nil
	}

	for idx := range plugins {
		plugin := &plugins[idx]

		if !slices.Contains(plugin.reconcilerCapabilities, kind) {
			continue
		}

		result, err := executeRequest(ctx, plugin.reconcilerHooksClient, request)
		if err != nil {
			return ctrl.Result{}, err
		}

		switch result.Behavior {
		case reconciler.ReconcilerHooksResult_BEHAVIOR_TERMINATE:
			return ctrl.Result{}, nil

		case reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE:
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Second * time.Duration(result.GetRequeueAfter()),
			}, nil

		case reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE:
		}
	}

	return ctrl.Result{}, nil
}
