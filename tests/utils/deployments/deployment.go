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

// Package deployments contains functions to control deployments
package deployments

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v5"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsReady checks if a Deployment is ready
func IsReady(deployment appsv1.Deployment) bool {
	// If the deployment has been scaled down to 0 replicas, we consider it ready
	if deployment.Status.Replicas == 0 && *deployment.Spec.Replicas == 0 {
		return true
	}

	if deployment.Status.ObservedGeneration < deployment.Generation ||
		deployment.Status.UpdatedReplicas < deployment.Status.Replicas ||
		deployment.Status.AvailableReplicas < deployment.Status.Replicas ||
		deployment.Status.ReadyReplicas < deployment.Status.Replicas {
		return false
	}

	if deployment.Status.Conditions == nil {
		return false
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status != "True" {
			return false
		}
		if condition.Type == appsv1.DeploymentProgressing && condition.Status != "True" {
			return false
		}
	}
	return true
}

// WaitForReady waits for a Deployment to be ready
func WaitForReady(
	ctx context.Context,
	crudClient client.Client,
	deployment *appsv1.Deployment,
	timeoutSeconds uint,
) error {
	err := retry.New(
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				if err := crudClient.Get(ctx, client.ObjectKey{
					Namespace: deployment.Namespace,
					Name:      deployment.Name,
				}, deployment); err != nil {
					return err
				}
				if !IsReady(*deployment) {
					return fmt.Errorf(
						"deployment not ready. Namespace: %v, Name: %v",
						deployment.Namespace,
						deployment.Name,
					)
				}
				return nil
			},
		)
	return err
}
