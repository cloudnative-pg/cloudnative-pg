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

package report

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

var errNoOperatorDeployment = fmt.Errorf("no deployment found")

// getOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
func getOperatorDeployment(ctx context.Context) (appsv1.Deployment, error) {
	deployment, err := tryGetOperatorDeployment(ctx,
		ctrlclient.MatchingLabels{labelOperatorNameKey: labelOperatorName},
		ctrlclient.InNamespace(plugin.Namespace))
	if err != errNoOperatorDeployment {
		return deployment, err
	}

	deployment, err = tryGetOperatorDeployment(ctx,
		ctrlclient.HasLabels{labelOperatorKeyPrefix + "openshift-operators"},
		ctrlclient.InNamespace(plugin.Namespace))
	if err != errNoOperatorDeployment {
		return deployment, err
	}

	deployment, err = tryGetOperatorDeployment(ctx,
		ctrlclient.HasLabels{labelOperatorKeyPrefix + plugin.Namespace},
		ctrlclient.InNamespace(plugin.Namespace))
	if err == errNoOperatorDeployment {
		return appsv1.Deployment{},
			fmt.Errorf("could not get operator in namespace '%s': %w",
				plugin.Namespace, err)
	}
	if err != nil {
		return appsv1.Deployment{}, err
	}

	return deployment, nil
}

// tryGetOperatorDeployment tries to fetch the operator deployment from the
// configured namespace
// May error with errNoOperatorDeployment if the deployment was not found
func tryGetOperatorDeployment(ctx context.Context, options ...ctrlclient.ListOption) (appsv1.Deployment, error) {
	deploymentList := &appsv1.DeploymentList{}

	if err := plugin.Client.List(ctx, deploymentList, options...); err != nil {
		return appsv1.Deployment{}, err
	}
	// We check if we have one or more deployments
	if len(deploymentList.Items) > 1 {
		return appsv1.Deployment{}, fmt.Errorf("number of operator deployments bigger than 1")
	}

	if len(deploymentList.Items) == 1 {
		return deploymentList.Items[0], nil
	}

	return appsv1.Deployment{}, errNoOperatorDeployment
}

// getOperatorPods returns the operator pods if found, error otherwise
func getOperatorPods(ctx context.Context) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}

	// This will work for newer version of the operator, which are using
	// our custom label
	if err := plugin.Client.List(
		ctx, podList,
		ctrlclient.MatchingLabels{"app.kubernetes.io/name": labelOperatorName},
		ctrlclient.InNamespace(plugin.Namespace)); err != nil {
		return nil, err
	}

	if len(podList.Items) > 0 {
		return podList.Items, nil
	}

	return nil, fmt.Errorf("operator pods not found")
}

// getOperatorSecrets returns the secrets used by the operator
func getOperatorSecrets(ctx context.Context, deployment appsv1.Deployment) ([]corev1.Secret, error) {
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

// getOperatorConfigMaps returns the configmap referenced by the operator
func getOperatorConfigMaps(ctx context.Context, deployment appsv1.Deployment) ([]corev1.ConfigMap, error) {
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
