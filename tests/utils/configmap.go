/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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
