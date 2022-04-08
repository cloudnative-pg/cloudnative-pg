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

package utils

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/controller"
)

// GetLeaderInfoFromConfigMap gathers leader holderIdentity annotation the operator configMap
func GetLeaderInfoFromConfigMap(operatorNamespace string, env *TestingEnvironment) (string, error) {
	// Leader election id is referred as configMap name for store leader details
	leaderElectionID := controller.LeaderElectionID
	configMapList := &corev1.ConfigMapList{}
	err := GetObjectList(env, configMapList, ctrlclient.InNamespace(operatorNamespace),
		ctrlclient.MatchingFields{"metadata.name": leaderElectionID},
	)
	if err != nil {
		return "", err
	}

	if len(configMapList.Items) != 1 {
		err := fmt.Errorf("configMapList item length is not 1: %v", len(configMapList.Items))
		return "", err
	}

	const leaderAnnotation = "control-plane.alpha.kubernetes.io/leader"
	if annotationInfo, ok := configMapList.Items[0].ObjectMeta.Annotations[leaderAnnotation]; ok {
		mapAnnotationInfo := make(map[string]interface{})
		if err = json.Unmarshal([]byte(annotationInfo), &mapAnnotationInfo); err != nil {
			return "", err
		}
		for key, value := range mapAnnotationInfo {
			if key == "holderIdentity" {
				return fmt.Sprintf("%v", value), nil
			}
		}
		return "", fmt.Errorf("no holderIdentity key found in %v", leaderAnnotation)
	}
	return "", fmt.Errorf("no %v annotation found", leaderAnnotation)
}
