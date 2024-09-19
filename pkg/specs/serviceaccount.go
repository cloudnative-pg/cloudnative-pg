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

package specs

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// UpdateServiceAccount sets the needed values in the ServiceAccount that will be used in every Pod
func UpdateServiceAccount(imagePullSecretsNames []string, serviceAccount *corev1.ServiceAccount) error {
	if serviceAccount.ImagePullSecrets == nil {
		serviceAccount.ImagePullSecrets = []corev1.LocalObjectReference{}
	}

	var newReferences []corev1.LocalObjectReference
	for _, name := range imagePullSecretsNames {
		found := false
		for _, existing := range serviceAccount.ImagePullSecrets {
			if name == existing.Name {
				found = true
				break
			}
		}
		if !found {
			newReferences = append(newReferences, corev1.LocalObjectReference{Name: name})
		}
	}
	serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, newReferences...)

	annotationValue, err := CreateManagedSecretsAnnotationValue(imagePullSecretsNames)
	if err != nil {
		return err
	}

	if serviceAccount.Annotations == nil {
		serviceAccount.Annotations = map[string]string{}
	}
	serviceAccount.Annotations[utils.OperatorManagedSecretsAnnotationName] = annotationValue

	return nil
}

// CreateManagedSecretsAnnotationValue creates the value of the annotations that stores
// the names of the secrets managed by the operator inside a ServiceAccount
func CreateManagedSecretsAnnotationValue(imagePullSecretsNames []string) (string, error) {
	result, err := json.Marshal(imagePullSecretsNames)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// IsServiceAccountAligned compares the given list of pull secrets with the
// ones managed by the operator inside the given ServiceAccount and returns
// true when everything is aligned
func IsServiceAccountAligned(
	ctx context.Context,
	sa *corev1.ServiceAccount,
	imagePullSecretsNames []string,
	updatedMetadata metav1.ObjectMeta,
) bool {
	contextLogger := log.FromContext(ctx)
	// This is an old version of the ServiceAccount, that need to be refreshed to
	// store the annotation value
	if sa.Annotations == nil {
		return false
	}

	value := sa.Annotations[utils.OperatorManagedSecretsAnnotationName]
	if value == "" {
		return false
	}

	var serviceAccountPullSecrets []string
	if err := json.Unmarshal([]byte(value), &serviceAccountPullSecrets); err != nil {
		contextLogger.Error(err, "Cannot detect if a ServiceAccount need to be refreshed or not, refreshing it",
			"serviceAccount", sa)
		return false
	}

	if len(serviceAccountPullSecrets) != len(imagePullSecretsNames) {
		return false
	}
	if serviceAccountPullSecrets != nil && imagePullSecretsNames != nil && !reflect.DeepEqual(
		serviceAccountPullSecrets, imagePullSecretsNames) {
		return false
	}

	for name, value := range updatedMetadata.Labels {
		if sa.Labels[name] != value {
			return false
		}
	}

	for name, value := range updatedMetadata.Annotations {
		if sa.Annotations[name] != value {
			return false
		}
	}

	return true
}
