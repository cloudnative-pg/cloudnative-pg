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
	"bytes"
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/controller"
)

// GetCNPsMutatingWebhookByName get the MutatingWebhook filtered by the name of one
// of the webhooks
func GetCNPsMutatingWebhookByName(env *TestingEnvironment, name string) (
	*admissionregistrationv1.MutatingWebhookConfiguration, int, error,
) {
	var mWebhooks admissionregistrationv1.MutatingWebhookConfigurationList
	err := GetObjectList(env, &mWebhooks)
	if err != nil {
		return nil, 0, err
	}

	for i, item := range mWebhooks.Items {
		for i2, webhook := range item.Webhooks {
			if webhook.Name == name {
				return &mWebhooks.Items[i], i2, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("mutating webhook not found")
}

// UpdateCNPsMutatingWebhookConf update MutatingWebhookConfiguration object
func UpdateCNPsMutatingWebhookConf(env *TestingEnvironment,
	wh *admissionregistrationv1.MutatingWebhookConfiguration,
) error {
	ctx := context.Background()
	_, err := env.Interface.AdmissionregistrationV1().
		MutatingWebhookConfigurations().Update(ctx, wh, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// GetCNPsValidatingWebhookConf get the ValidatingWebhook linked to the operator
func GetCNPsValidatingWebhookConf(env *TestingEnvironment) (
	*admissionregistrationv1.ValidatingWebhookConfiguration, error,
) {
	ctx := context.Background()
	validatingWebhookConfig, err := env.Interface.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
		ctx, controller.ValidatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return validatingWebhookConfig, nil
}

// GetCNPsValidatingWebhookByName get ValidatingWebhook by the name of one
// of the webhooks
func GetCNPsValidatingWebhookByName(env *TestingEnvironment, name string) (
	*admissionregistrationv1.ValidatingWebhookConfiguration, int, error,
) {
	var vWebhooks admissionregistrationv1.ValidatingWebhookConfigurationList
	err := GetObjectList(env, &vWebhooks)
	if err != nil {
		return nil, 0, err
	}

	for i, item := range vWebhooks.Items {
		for i2, webhook := range item.Webhooks {
			if webhook.Name == name {
				return &vWebhooks.Items[i], i2, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("validating webhook not found")
}

// UpdateCNPsValidatingWebhookConf update the ValidatingWebhook object
func UpdateCNPsValidatingWebhookConf(env *TestingEnvironment,
	wh *admissionregistrationv1.ValidatingWebhookConfiguration,
) error {
	ctx := context.Background()
	_, err := env.Interface.AdmissionregistrationV1().
		ValidatingWebhookConfigurations().Update(ctx, wh, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// CheckWebhookReady ensures that the operator has finished the webhook setup.
func CheckWebhookReady(env *TestingEnvironment, namespace string) error {
	// Check CA
	secret := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      controller.WebhookSecretName,
	}
	err := GetObject(env, secretNamespacedName, secret)
	if err != nil {
		return err
	}

	ca := secret.Data["tls.crt"]

	mutatingWebhookConfig, err := env.GetCNPsMutatingWebhookConf()
	if err != nil {
		return err
	}

	for _, webhook := range mutatingWebhookConfig.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret %+v not match with ca bundle in %v: %v is not equal to %v",
				controller.MutatingWebhookConfigurationName, secret, string(ca), string(webhook.ClientConfig.CABundle))
		}
	}

	validatingWebhookConfig, err := GetCNPsValidatingWebhookConf(env)
	if err != nil {
		return err
	}

	for _, webhook := range validatingWebhookConfig.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret not match with ca bundle in %v",
				controller.ValidatingWebhookConfigurationName)
		}
	}

	customResourceDefinitionsName := []string{
		"backups.postgresql.k8s.enterprisedb.io",
		"clusters.postgresql.k8s.enterprisedb.io",
		"scheduledbackups.postgresql.k8s.enterprisedb.io",
	}

	ctx := context.Background()
	for _, c := range customResourceDefinitionsName {
		crd, err := env.APIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, c, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if crd.Spec.Conversion == nil {
			continue
		}

		if crd.Spec.Conversion.Strategy == v1.NoneConverter {
			continue
		}

		if crd.Spec.Conversion.Webhook != nil && crd.Spec.Conversion.Webhook.ClientConfig != nil &&
			!bytes.Equal(crd.Spec.Conversion.Webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret not match with ca bundle in %v; %v not equal to %v", c,
				string(crd.Spec.Conversion.Webhook.ClientConfig.CABundle), string(ca))
		}
	}
	return nil
}

// GetCNPsMutatingWebhookConf get the MutatingWebhook linked to the operator
func (env TestingEnvironment) GetCNPsMutatingWebhookConf() (
	*admissionregistrationv1.MutatingWebhookConfiguration, error,
) {
	ctx := context.Background()
	return env.Interface.AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(ctx, controller.MutatingWebhookConfigurationName, metav1.GetOptions{})
}
