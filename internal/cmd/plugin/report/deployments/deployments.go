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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

const (
	webhookSecretName       = "cnpg-webhook-cert"              // #nosec
	caSecretName            = "cnpg-ca-secret"                 // #nosec
	defaultConfigSecretName = "cnpg-controller-manager-config" // #nosec
	defaultConfigmapName    = "cnpg-controller-manager-config"
	labelOperatorNameKey    = "app.kubernetes.io/name"
	labelOperatorName       = "cloudnative-pg"
	labelOperatorKeyPrefix  = "operators.coreos.com/cloudnative-pg."
)

// GetOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
func GetOperatorDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	deployment, err := tryGetOperatorDeployment(ctx, ctrlclient.MatchingLabels{labelOperatorNameKey: labelOperatorName})
	if err != nil || deployment != nil {
		return deployment, err
	}

	deployment, err = tryGetOperatorDeployment(ctx, ctrlclient.HasLabels{labelOperatorKeyPrefix + "openshift-operators"})
	if err != nil || deployment != nil {
		return deployment, err
	}

	namespace, err := getOperatorPodNamespace(ctx)
	if err != nil {
		return nil, err
	}

	deployment, err = tryGetOperatorDeployment(ctx, ctrlclient.HasLabels{labelOperatorKeyPrefix + namespace})
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, fmt.Errorf("no operator deployments found under namespace %v", namespace)
	}

	return deployment, nil
}

func tryGetOperatorDeployment(ctx context.Context, options ...ctrlclient.ListOption) (*appsv1.Deployment, error) {
	deploymentList := &appsv1.DeploymentList{}

	if err := plugin.Client.List(ctx, deploymentList, options...); err != nil {
		return nil, err
	}
	// We check if we have one or more deployments
	if len(deploymentList.Items) > 1 {
		return nil, fmt.Errorf("number of operator deployments bigger than 1")
	}

	if len(deploymentList.Items) == 1 {
		return &deploymentList.Items[0], nil
	}

	return nil, nil
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

// GetOperatorSecrets returns the secrets used by the operator
func GetOperatorSecrets(ctx context.Context, deployment appsv1.Deployment) ([]corev1.Secret, error) {
	contextLogger := log.FromContext(ctx)
	operatorNamespace := deployment.GetNamespace()
	// default secrets name
	secretNames := []string{
		webhookSecretName,
		caSecretName,
	}
	// get the operator config secrets name from deployment
	configSecretName, err := getOperatorConfigSecretName(deployment)
	if err != nil {
		return nil, err
	}
	if configSecretName != "" {
		secretNames = append(secretNames, configSecretName)
	} else {
		secretNames = append(secretNames, defaultConfigSecretName)
	}
	secrets := make([]corev1.Secret, 0, len(secretNames))
	for _, ss := range secretNames {
		var secret corev1.Secret

		err := plugin.Client.Get(ctx, types.NamespacedName{Name: ss, Namespace: operatorNamespace}, &secret)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			contextLogger.Warning("could not get secret")
			return nil, err
		}

		secrets = append(secrets, secret)
	}
	return secrets, nil
}

// GetOperatorConfigMaps returns the configmap referenced by the operator
func GetOperatorConfigMaps(ctx context.Context, deployment appsv1.Deployment) ([]corev1.ConfigMap, error) {
	contextLogger := log.FromContext(ctx)

	var configMaps []string
	operatorNamespace := deployment.GetNamespace()

	// get the operator configmap name from deployment
	configMapName, err := getOperatorConfigMapName(deployment)
	if err != nil {
		return nil, err
	}
	if configMapName != "" {
		configMaps = append(configMaps, configMapName)
	} else {
		configMaps = append(configMaps, defaultConfigmapName)
	}
	configs := make([]corev1.ConfigMap, 0, len(configMaps))
	for _, cm := range configMaps {
		var config corev1.ConfigMap
		err := plugin.Client.Get(ctx, types.NamespacedName{Name: cm, Namespace: operatorNamespace}, &config)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			contextLogger.Warning("could not get secret")
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// getOperatorConfigMapName return the name of configmap for operator configuration
func getOperatorConfigMapName(deployment appsv1.Deployment) (string, error) {
	const configMapArgPrefix = "--config-map-name="
	container := getManagerContainer(deployment)
	if container == nil {
		err := fmt.Errorf("can not find manager container from deployment")
		return "", err
	}
	var configMapName string
	for _, arg := range container.Args {
		if strings.HasPrefix(arg, configMapArgPrefix) {
			configMapName = strings.Split(arg, "=")[1]
			break
		}
	}
	return configMapName, nil
}

// getOperatorConfigSecretName return the name of secret for operator configuration
func getOperatorConfigSecretName(deployment appsv1.Deployment) (string, error) {
	const secretArgPrefix = "--secret-name="
	container := getManagerContainer(deployment)
	if container == nil {
		err := fmt.Errorf("can not find manager container from deployment")
		return "", err
	}
	var secretName string
	for _, arg := range container.Args {
		if strings.HasPrefix(arg, secretArgPrefix) {
			secretName = strings.Split(arg, "=")[1]
			break
		}
	}
	return secretName, nil
}

// getManagerContainer get the running container from the deployment spec
func getManagerContainer(deployment appsv1.Deployment) *corev1.Container {
	const managerContainerName = "manager"
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Name == managerContainerName {
			return &c
		}
	}
	return nil
}

// getOperatorPodNamespace get the operator namespace from pod
func getOperatorPodNamespace(ctx context.Context) (string, error) {
	operatorPods, err := GetOperatorPods(ctx)
	if err != nil {
		return "", err
	}

	if len(operatorPods) < 1 {
		return "", fmt.Errorf("can not find namespace with operator deployments installed")
	}

	return operatorPods[0].Namespace, nil
}
