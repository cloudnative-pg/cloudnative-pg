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

// Package deployments contains code to get operator deployment
package deployments

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

const labelOperatorName = "cloudnative-pg"

// GetOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
func GetOperatorDeployment(ctx context.Context) (appsv1.Deployment, error) {
	deploymentList := &appsv1.DeploymentList{}

	if err := plugin.Client.List(
		ctx, deploymentList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": labelOperatorName},
	); err != nil {
		return appsv1.Deployment{}, err
	}
	// We check if we have one or more deployments
	if len(deploymentList.Items) > 1 || len(deploymentList.Items) < 1 {
		err := fmt.Errorf("number of operator deployments not equal to 1")
		return appsv1.Deployment{}, err
	}

	if len(deploymentList.Items) == 1 {
		return deploymentList.Items[0], nil
	}

	if err := plugin.Client.List(
		ctx,
		deploymentList,
		ctrlclient.HasLabels{"operators.coreos.com/cloudnative-pg.openshift-operators"},
	); err != nil {
		return appsv1.Deployment{}, err
	}

	// We check if we have one or more deployments
	if len(deploymentList.Items) > 1 || len(deploymentList.Items) < 1 {
		err := fmt.Errorf("number of operator deployments not equal to 1")
		return appsv1.Deployment{}, err
	}

	return deploymentList.Items[0], nil
}

// GetOperatorPods returns the operator pods if found, error otherwise
func GetOperatorPods(ctx context.Context) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}

	// This will work for newer version of the operator, which are using
	// our custom label
	if err := plugin.Client.List(
		ctx, podList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": labelOperatorName}); err != nil {
		return nil, err
	}

	if len(podList.Items) > 0 {
		return podList.Items, nil
	}

	return nil, fmt.Errorf("operator pods not found")
}

// GetOperatorNamespaceName returns the namespace the operator Deployment is running in
func GetOperatorNamespaceName(ctx context.Context) (string, error) {
	deployment, err := GetOperatorDeployment(ctx)
	if err != nil {
		return "", err
	}
	return deployment.GetNamespace(), err
}
