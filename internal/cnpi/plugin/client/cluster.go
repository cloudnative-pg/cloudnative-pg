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

	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	"github.com/cloudnative-pg/machinery/pkg/log"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (data *data) MutateCluster(ctx context.Context, object client.Object, mutatedObject client.Object) error {
	err := data.innerMutateCluster(ctx, object, mutatedObject)
	return wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerMutateCluster(ctx context.Context, object client.Object, mutatedObject client.Object) error {
	contextLogger := log.FromContext(ctx)

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.OperatorCapabilities(), operator.OperatorCapability_RPC_TYPE_MUTATE_CLUSTER) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := operator.OperatorMutateClusterRequest{
			Definition: serializedObject,
		}

		pluginLogger.Trace("Calling MutateCluster endpoint", "definition", request.Definition)
		result, err := plugin.OperatorClient().MutateCluster(ctx, &request)
		if err != nil {
			pluginLogger.Error(err, "Error while calling MutateCluster")
			return err
		}

		if len(result.JsonPatch) == 0 {
			// There's nothing to mutate
			continue
		}

		patch, err := jsonpatch.DecodePatch(result.JsonPatch)
		if err != nil {
			pluginLogger.Error(err, "Error while decoding JSON patch from plugin", "patch", result.JsonPatch)
			return err
		}

		mutatedObject, err := patch.Apply(serializedObject)
		if err != nil {
			pluginLogger.Error(err, "Error while applying JSON patch from plugin", "patch", result.JsonPatch)
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

var (
	errInvalidJSON        = newPluginError("invalid json")
	errSetStatusInCluster = newPluginError("SetStatusInCluster invocation failed")
)

func (data *data) SetStatusInCluster(ctx context.Context, cluster client.Object) (map[string]string, error) {
	m, err := data.innerSetStatusInCluster(ctx, cluster)
	return m, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerSetStatusInCluster(ctx context.Context, cluster client.Object) (map[string]string, error) {
	contextLogger := log.FromContext(ctx)
	serializedObject, err := json.Marshal(cluster)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			cluster.GetObjectKind().GroupVersionKind().Kind,
			cluster.GetNamespace(), cluster.GetName(),
			err,
		)
	}

	pluginStatuses := make(map[string]string)
	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.OperatorCapabilities(), operator.OperatorCapability_RPC_TYPE_SET_STATUS_IN_CLUSTER) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := operator.SetStatusInClusterRequest{
			Cluster: serializedObject,
		}

		pluginLogger.Trace("Calling SetStatusInCluster endpoint")
		response, err := plugin.OperatorClient().SetStatusInCluster(ctx, &request)
		if err != nil {
			pluginLogger.Error(err, "Error while calling SetStatusInCluster")
			return nil, fmt.Errorf("%w: %w", errSetStatusInCluster, err)
		}

		if len(response.JsonStatus) == 0 {
			contextLogger.Trace("json status is empty, skipping it", "pluginName", plugin.Name())
			continue
		}
		if err := json.Unmarshal(response.JsonStatus, &map[string]any{}); err != nil {
			contextLogger.Error(err, "found a malformed json while evaluating SetStatusInCluster response",
				"pluginName", plugin.Name())
			return nil, fmt.Errorf("%w: %w", errInvalidJSON, err)
		}

		pluginStatuses[plugin.Name()] = string(response.JsonStatus)
	}

	return pluginStatuses, nil
}

func (data *data) ValidateClusterCreate(
	ctx context.Context,
	object client.Object,
) (field.ErrorList, error) {
	result, err := data.innerValidateClusterCreate(ctx, object)
	return result, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerValidateClusterCreate(
	ctx context.Context,
	object client.Object,
) (field.ErrorList, error) {
	contextLogger := log.FromContext(ctx)

	serializedObject, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			object.GetObjectKind().GroupVersionKind().Kind,
			object.GetNamespace(), object.GetName(),
			err,
		)
	}

	var validationErrors []*operator.ValidationError
	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.OperatorCapabilities(), operator.OperatorCapability_RPC_TYPE_VALIDATE_CLUSTER_CREATE) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := operator.OperatorValidateClusterCreateRequest{
			Definition: serializedObject,
		}

		pluginLogger.Trace("Calling ValidatedClusterCreate endpoint", "definition", request.Definition)
		result, err := plugin.OperatorClient().ValidateClusterCreate(ctx, &request)
		if err != nil {
			pluginLogger.Error(err, "Error while calling ValidatedClusterCreate")
			return nil, err
		}

		validationErrors = append(validationErrors, result.ValidationErrors...)
	}

	return validationErrorsToErrorList(validationErrors), nil
}

func (data *data) ValidateClusterUpdate(
	ctx context.Context,
	oldObject client.Object,
	newObject client.Object,
) (field.ErrorList, error) {
	result, err := data.innerValidateClusterUpdate(ctx, oldObject, newObject)
	return result, wrapAsPluginErrorIfNeeded(err)
}

func (data *data) innerValidateClusterUpdate(
	ctx context.Context,
	oldObject client.Object,
	newObject client.Object,
) (field.ErrorList, error) {
	contextLogger := log.FromContext(ctx)

	serializedOldObject, err := json.Marshal(oldObject)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			oldObject.GetObjectKind().GroupVersionKind().Kind,
			oldObject.GetNamespace(), oldObject.GetName(),
			err,
		)
	}

	serializedNewObject, err := json.Marshal(newObject)
	if err != nil {
		return nil, fmt.Errorf("while serializing %s %s/%s to JSON: %w",
			newObject.GetObjectKind().GroupVersionKind().Kind,
			newObject.GetNamespace(), newObject.GetName(),
			err,
		)
	}

	var validationErrors []*operator.ValidationError
	for idx := range data.plugins {
		plugin := data.plugins[idx]

		if !slices.Contains(plugin.OperatorCapabilities(), operator.OperatorCapability_RPC_TYPE_VALIDATE_CLUSTER_CHANGE) {
			continue
		}

		pluginLogger := contextLogger.WithValues("pluginName", plugin.Name())
		request := operator.OperatorValidateClusterChangeRequest{
			OldCluster: serializedOldObject,
			NewCluster: serializedNewObject,
		}

		pluginLogger.Trace(
			"Calling ValidateClusterChange endpoint",
			"oldCluster", request.OldCluster,
			"newCluster", request.NewCluster)
		result, err := plugin.OperatorClient().ValidateClusterChange(ctx, &request)
		if err != nil {
			pluginLogger.Error(err, "Error while calling ValidatedClusterCreate")
			return nil, err
		}

		validationErrors = append(validationErrors, result.ValidationErrors...)
	}

	return validationErrorsToErrorList(validationErrors), nil
}

// validationErrorsToErrorList makes up a list of validation errors as required by
// the Kubernetes API from the GRPC plugin interface types
func validationErrorsToErrorList(validationErrors []*operator.ValidationError) (result field.ErrorList) {
	result = make(field.ErrorList, len(validationErrors))
	for i, validationError := range validationErrors {
		result[i] = field.Invalid(
			field.NewPath(validationError.PathComponents[0], validationError.PathComponents[1:]...),
			validationError.Value,
			validationError.Message,
		)
	}

	return result
}
