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
	"fmt"
	"reflect"
	"slices"

	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/machinery/pkg/log"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"
)

var runtimeScheme = runtime.NewScheme()

func init() {
	_ = scheme.AddToScheme(runtimeScheme)
}

func (data *data) LifecycleHook(
	ctx context.Context,
	operationType plugin.OperationVerb,
	cluster client.Object,
	object client.Object,
) (client.Object, error) {
	obj, err := data.innerLifecycleHook(ctx, operationType, cluster, object)
	return obj, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerLifecycleHook(
	ctx context.Context,
	operationType plugin.OperationVerb,
	cluster client.Object,
	object client.Object,
) (client.Object, error) {
	contextLogger := log.FromContext(ctx).WithName("lifecycle_hook")

	typedOperationType, err := operationType.ToOperationType_Type()
	if err != nil {
		return nil, err
	}
	gvk := object.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		gvk, err = apiutil.GVKForObject(object, runtimeScheme)
		if err != nil {
			// Skip unknown object, but returning the same object so the reconcile can continue
			contextLogger.Debug("skipping unknown object", "object", object, "error", err)
			return object, nil
		}
	}
	object.GetObjectKind().SetGroupVersionKind(gvk)

	var invokablePlugin []connection.Interface
	for _, plg := range data.plugins {
		for _, capability := range plg.LifecycleCapabilities() {
			if capability.Group != gvk.Group || capability.Kind != gvk.Kind {
				continue
			}

			contained := slices.ContainsFunc(capability.OperationTypes, func(ot *lifecycle.OperatorOperationType) bool {
				return ot.GetType() == typedOperationType
			})

			if !contained {
				continue
			}

			invokablePlugin = append(invokablePlugin, plg)
		}
	}

	if len(invokablePlugin) == 0 {
		return object, nil
	}

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	serializedObjectOrig := make([]byte, len(serializedObject))
	copy(serializedObjectOrig, serializedObject)
	for _, plg := range invokablePlugin {
		req := &lifecycle.OperatorLifecycleRequest{
			OperationType: &lifecycle.OperatorOperationType{
				Type: typedOperationType,
			},
			ClusterDefinition: serializedCluster,
			ObjectDefinition:  serializedObject,
		}
		result, err := plg.LifecycleClient().LifecycleHook(ctx, req)
		if err != nil {
			contextLogger.Error(err, "Error while calling LifecycleHook")
			return nil, err
		}

		if result == nil || len(result.JsonPatch) == 0 {
			// There's nothing to mutate
			continue
		}

		patch, err := jsonpatch.DecodePatch(result.JsonPatch)
		if err != nil {
			contextLogger.Error(err, "Error while decoding JSON patch from plugin", "patch", result.JsonPatch)
			return nil, err
		}

		responseObj, err := patch.Apply(serializedObject)
		if err != nil {
			contextLogger.Error(err, "Error while applying JSON patch from plugin", "patch", result.JsonPatch)
			return nil, err
		}

		serializedObject = responseObj
	}

	if reflect.DeepEqual(serializedObject, serializedObjectOrig) {
		return object, nil
	}

	decoder := scheme.Codecs.UniversalDeserializer()
	mutatedObject, _, err := decoder.Decode(serializedObject, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("while deserializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	return mutatedObject.(client.Object), nil
}
