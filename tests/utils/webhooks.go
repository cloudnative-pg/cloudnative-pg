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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
)

// GetCNPGsMutatingWebhookByName get the MutatingWebhook filtered by the name of one
// of the webhooks
func GetCNPGsMutatingWebhookByName(env *TestingEnvironment, name string) (
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

// UpdateCNPGsMutatingWebhookConf update MutatingWebhookConfiguration object
func UpdateCNPGsMutatingWebhookConf(env *TestingEnvironment,
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

// GetCNPGsValidatingWebhookConf get the ValidatingWebhook linked to the operator
func GetCNPGsValidatingWebhookConf(env *TestingEnvironment) (
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

// GetCNPGsValidatingWebhookByName get ValidatingWebhook by the name of one
// of the webhooks
func GetCNPGsValidatingWebhookByName(env *TestingEnvironment, name string) (
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

// UpdateCNPGsValidatingWebhookConf update the ValidatingWebhook object
func UpdateCNPGsValidatingWebhookConf(env *TestingEnvironment,
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

	mutatingWebhookConfig, err := env.GetCNPGsMutatingWebhookConf()
	if err != nil {
		return err
	}

	for _, webhook := range mutatingWebhookConfig.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret %+v not match with ca bundle in %v: %v is not equal to %v",
				controller.MutatingWebhookConfigurationName, secret, string(ca), string(webhook.ClientConfig.CABundle))
		}
	}

	validatingWebhookConfig, err := GetCNPGsValidatingWebhookConf(env)
	if err != nil {
		return err
	}

	for _, webhook := range validatingWebhookConfig.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret not match with ca bundle in %v",
				controller.ValidatingWebhookConfigurationName)
		}
	}

	return nil
}

// GetCNPGsMutatingWebhookConf get the MutatingWebhook linked to the operator
func (env TestingEnvironment) GetCNPGsMutatingWebhookConf() (
	*admissionregistrationv1.MutatingWebhookConfiguration, error,
) {
	ctx := context.Background()
	return env.Interface.AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(ctx, controller.MutatingWebhookConfigurationName, metav1.GetOptions{})
}
