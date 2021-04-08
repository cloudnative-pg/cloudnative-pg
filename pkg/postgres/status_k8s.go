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

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// ExtractInstancesStatus run an "exec" call to the passed Pods and extract, using the
// instance manager, the status of the underlying PostgreSQL instance. If an Exec call
// cannot reach a Pod, the result list will contain errors
func ExtractInstancesStatus(
	ctx context.Context,
	config *rest.Config,
	filteredPods []corev1.Pod,
	postgresContainerName string,
) PostgresqlStatusList {
	var result PostgresqlStatusList

	for idx := range filteredPods {
		instanceStatus := getReplicaStatusFromPod(
			ctx, config, filteredPods[idx], postgresContainerName)
		instanceStatus.IsReady = utils.IsPodReady(filteredPods[idx])
		result.Items = append(result.Items, instanceStatus)
	}

	return result
}

func getReplicaStatusFromPod(
	ctx context.Context,
	config *rest.Config,
	pod corev1.Pod,
	postgresContainerName string) PostgresqlStatus {
	result := PostgresqlStatus{
		PodName: pod.Name,
	}

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
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	err = json.Unmarshal([]byte(stdout), &result)
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	return result
}
