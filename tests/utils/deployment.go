/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentIsReady checks if a Deployment is ready
func DeploymentIsReady(deployment appsv1.Deployment) bool {
	return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas
}

// DeploymentWaitForReady waits for a Deployment to be ready
func DeploymentWaitForReady(env *TestingEnvironment, deployment *appsv1.Deployment, timeoutSeconds uint) error {
	err := retry.Do(
		func() error {
			if err := env.Client.Get(env.Ctx, client.ObjectKey{
				Namespace: deployment.Namespace,
				Name:      deployment.Name,
			}, deployment); err != nil {
				return err
			}
			if !DeploymentIsReady(*deployment) {
				return fmt.Errorf(
					"deployment not ready. Namespace: %v, Name: %v",
					deployment.Namespace,
					deployment.Name,
				)
			}
			return nil
		},
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay),
	)
	return err
}
