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

	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

func (data *data) MutatePod(
	ctx context.Context,
	cluster client.Object,
	object client.Object,
	mutatedObject client.Object,
) error {
	contextLogger := log.FromContext(ctx)

	serializedCluster, err := json.Marshal(cluster)
	if err != nil {
		return fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	for idx := range data.plugins {
		plugin := &data.plugins[idx]

		if !slices.Contains(plugin.operatorCapabilities, operator.OperatorCapability_RPC_TYPE_MUTATE_POD) {
			continue
		}

		contextLogger := contextLogger.WithValues(
			"pluginName", plugin.name,
		)
		request := operator.OperatorMutatePodRequest{
			ClusterDefinition: serializedCluster,
			PodDefinition:     serializedObject,
		}

		contextLogger.Trace(
			"Calling MutatePod endpoint",
			"clusterDefinition", request.ClusterDefinition,
			"podDefinition", request.PodDefinition)
		result, err := plugin.operatorClient.MutatePod(ctx, &request)
		if err != nil {
			contextLogger.Error(err, "Error while calling MutatePod")
			return err
		}

		if result == nil || len(result.JsonPatch) == 0 {
			// There's nothing to mutate
			continue
		}

		mutatedObject, err := jsonpatch.MergePatch(serializedObject, result.JsonPatch)
		if err != nil {
			contextLogger.Error(err, "Error while applying JSON patch from plugin", "patch", result.JsonPatch)
			return err
		}

		serializedObject = mutatedObject
	}

	if err := json.Unmarshal(serializedObject, mutatedObject); err != nil {
		return fmt.Errorf("while deserializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	return nil
}
