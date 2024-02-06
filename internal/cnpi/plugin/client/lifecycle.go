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

	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

func (data *data) LifecycleHook(
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
	var invokablePlugin []pluginData
	for _, plg := range data.plugins {
		for _, capability := range plg.lifecycleCapabilities {
			if capability.Group != gvk.Group || capability.Kind != gvk.Kind {
				continue
			}

			contained := slices.ContainsFunc(capability.OperationType, func(ot *lifecycle.OperationType) bool {
				return ot.GetType() == typedOperationType
			})

			if !contained {
				continue
			}

			invokablePlugin = append(invokablePlugin, plg)
		}
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

	for _, plg := range invokablePlugin {
		req := &lifecycle.LifecycleRequest{
			OperationType: &lifecycle.OperationType{
				Type: typedOperationType,
			},
			ClusterDefinition: serializedCluster,
			ObjectDefinition:  serializedObject,
		}
		result, err := plg.lifecycleClient.LifecycleHook(ctx, req)
		if err != nil {
			contextLogger.Error(err, "Error while calling LifecycleHook")
			return nil, err
		}

		if result == nil || len(result.JsonPatch) == 0 {
			// There's nothing to mutate
			continue
		}

		responseObj, err := jsonpatch.MergePatch(serializedObject, result.JsonPatch)
		if err != nil {
			contextLogger.Error(err, "Error while applying JSON patch from plugin", "patch", result.JsonPatch)
			return nil, err
		}

		serializedObject = responseObj
	}

	if len(serializedObject) == 0 {
		return object, nil
	}

	mutatedObject := object.DeepCopyObject().(client.Object)
	if err := json.Unmarshal(serializedObject, mutatedObject); err != nil {
		return nil, fmt.Errorf("while deserializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	return mutatedObject, nil
}
