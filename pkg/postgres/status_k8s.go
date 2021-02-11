/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"context"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

var log = ctrl.Log.WithName("instance-status")

// ExtractInstancesStatus run an "exec" call to the passed Pods and extract, using the
// instance manager, the status of the underlying PostgreSQL instance
func ExtractInstancesStatus(
	ctx context.Context,
	config *rest.Config,
	filteredPods []corev1.Pod,
	postgresContainerName string,
) (PostgresqlStatusList, error) {
	var result PostgresqlStatusList

	for idx := range filteredPods {
		if utils.IsPodReady(filteredPods[idx]) {
			instanceStatus, err := getReplicaStatusFromPod(ctx, config, filteredPods[idx], postgresContainerName)
			if err != nil {
				log.Error(err, "Error while extracting instance status",
					"name", filteredPods[idx].Name,
					"namespace", filteredPods[idx].Namespace)
				return result, err
			}

			result.Items = append(result.Items, instanceStatus)
		}
	}

	return result, nil
}

func getReplicaStatusFromPod(
	ctx context.Context,
	config *rest.Config,
	pod corev1.Pod,
	postgresContainerName string) (PostgresqlStatus, error) {
	var result PostgresqlStatus

	timeout := time.Second * 2
	clientInterface := kubernetes.NewForConfigOrDie(config)
	stdout, _, err := utils.ExecCommand(
		ctx,
		clientInterface,
		config,
		pod,
		postgresContainerName,
		&timeout,
		"/controller/manager", "instance", "status")

	if err != nil {
		return result, err
	}

	err = json.Unmarshal([]byte(stdout), &result)
	if err != nil {
		return result, err
	}

	result.PodName = pod.Name
	return result, nil
}
