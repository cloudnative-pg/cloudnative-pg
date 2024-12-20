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

// Package deployments contains functions to control deployments
package deployments

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deploymentIsReady checks if a Deployment is ready
func deploymentIsReady(deployment appsv1.Deployment) bool {
	return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas
}

// DeploymentWaitForReady waits for a Deployment to be ready
func DeploymentWaitForReady(
	ctx context.Context,
	crudClient client.Client,
	deployment *appsv1.Deployment,
	timeoutSeconds uint,
) error {
	err := retry.Do(
		func() error {
			if err := crudClient.Get(ctx, client.ObjectKey{
				Namespace: deployment.Namespace,
				Name:      deployment.Name,
			}, deployment); err != nil {
				return err
			}
			if !deploymentIsReady(*deployment) {
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
