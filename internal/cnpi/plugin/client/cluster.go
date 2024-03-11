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
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

func (data *data) MutateCluster(ctx context.Context, object client.Object, mutatedObject client.Object) error {
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
		plugin := &data.plugins[idx]

		if !slices.Contains(plugin.operatorCapabilities, operator.OperatorCapability_RPC_TYPE_MUTATE_CLUSTER) {
			continue
		}

		contextLogger := contextLogger.WithValues(
			"pluginName", plugin.name,
		)
		request := operator.OperatorMutateClusterRequest{
			Definition: serializedObject,
		}

		contextLogger.Trace("Calling MutateCluster endpoint", "definition", request.Definition)
		result, err := plugin.operatorClient.MutateCluster(ctx, &request)
		if err != nil {
			contextLogger.Error(err, "Error while calling MutateCluster")
			return err
		}

		if len(result.JsonPatch) == 0 {
			// There's nothing to mutate
			continue
		}

		patch, err := jsonpatch.DecodePatch(result.JsonPatch)
		if err != nil {
			contextLogger.Error(err, "Error while decoding JSON patch from plugin", "patch", result.JsonPatch)
			return err
		}

		mutatedObject, err := patch.Apply(serializedObject)
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

func (data *data) ValidateClusterCreate(
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
		plugin := &data.plugins[idx]

		if !slices.Contains(plugin.operatorCapabilities, operator.OperatorCapability_RPC_TYPE_VALIDATE_CLUSTER_CREATE) {
			continue
		}

		contextLogger := contextLogger.WithValues(
			"pluginName", plugin.name,
		)
		request := operator.OperatorValidateClusterCreateRequest{
			Definition: serializedObject,
		}

		contextLogger.Trace("Calling ValidatedClusterCreate endpoint", "definition", request.Definition)
		result, err := plugin.operatorClient.ValidateClusterCreate(ctx, &request)
		if err != nil {
			contextLogger.Error(err, "Error while calling ValidatedClusterCreate")
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
		plugin := &data.plugins[idx]

		if !slices.Contains(plugin.operatorCapabilities, operator.OperatorCapability_RPC_TYPE_VALIDATE_CLUSTER_CHANGE) {
			continue
		}

		contextLogger := contextLogger.WithValues(
			"pluginName", plugin.name,
		)
		request := operator.OperatorValidateClusterChangeRequest{
			OldCluster: serializedOldObject,
			NewCluster: serializedNewObject,
		}

		contextLogger.Trace(
			"Calling ValidateClusterChange endpoint",
			"oldCluster", request.OldCluster,
			"newCluster", request.NewCluster)
		result, err := plugin.operatorClient.ValidateClusterChange(ctx, &request)
		if err != nil {
			contextLogger.Error(err, "Error while calling ValidatedClusterCreate")
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
