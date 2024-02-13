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

	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (data *data) PreReconcile(ctx context.Context, cluster client.Object) (ctrl.Result, error) {
	return reconcilerHook(
		ctx,
		cluster,
		data.plugins,
		operator.OperatorCapability_RPC_TYPE_PRE_RECONCILE,
		func(serializedCluster []byte) *operator.OperatorPreReconcileRequest {
			return &operator.OperatorPreReconcileRequest{
				ClusterDefinition: serializedCluster,
			}
		},
		func(
			ctx context.Context,
			plugin operator.OperatorClient,
			request *operator.OperatorPreReconcileRequest,
		) (hookResult, error) {
			return plugin.PreReconcile(ctx, request)
		},
	)
}

func (data *data) PostReconcile(ctx context.Context, cluster client.Object) (ctrl.Result, error) {
	return reconcilerHook(
		ctx,
		cluster,
		data.plugins,
		operator.OperatorCapability_RPC_TYPE_POST_RECONCILE,
		func(serializedCluster []byte) *operator.OperatorPostReconcileRequest {
			return &operator.OperatorPostReconcileRequest{
				ClusterDefinition: serializedCluster,
			}
		},
		func(
			ctx context.Context,
			plugin operator.OperatorClient,
			request *operator.OperatorPostReconcileRequest,
		) (hookResult, error) {
			return plugin.PostReconcile(ctx, request)
		},
	)
}

type hookResult interface {
	GetRequeue() bool
	GetRequeueAfter() int64
}

func reconcilerHook[T any](
	ctx context.Context,
	cluster client.Object,
	plugins []pluginData,
	capability operator.OperatorCapability_RPC_Type,
	createRequest func(cluster []byte) T,
	executeRequest func(ctx context.Context, plugin operator.OperatorClient, request T) (hookResult, error),
) (ctrl.Result, error) {
	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	request := createRequest(serializedCluster)
	for idx := range plugins {
		plugin := &plugins[idx]

		if !slices.Contains(plugin.operatorCapabilities, capability) {
			continue
		}

		result, err := executeRequest(ctx, plugin.operatorClient, request)
		if err != nil {
			return ctrl.Result{}, err
		}

		if result.GetRequeueAfter() > 0 {
			return ctrl.Result{RequeueAfter: time.Second * time.Duration(result.GetRequeueAfter())}, nil
		}
		if result.GetRequeue() {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}
