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

package operator

import (
	"bytes"
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// GetMutatingWebhookByName get the MutatingWebhook filtered by the name of one
// of the webhooks
func GetMutatingWebhookByName(
	ctx context.Context,
	crudClient client.Client,
	name string,
) (
	*admissionregistrationv1.MutatingWebhookConfiguration, int, error,
) {
	var mWebhooks admissionregistrationv1.MutatingWebhookConfigurationList
	err := objects.List(ctx, crudClient, &mWebhooks)
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

// UpdateMutatingWebhookConf update MutatingWebhookConfiguration object
func UpdateMutatingWebhookConf(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	wh *admissionregistrationv1.MutatingWebhookConfiguration,
) error {
	_, err := kubeInterface.AdmissionregistrationV1().
		MutatingWebhookConfigurations().Update(ctx, wh, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// getCNPGsValidatingWebhookConf get the ValidatingWebhook linked to the operator
func getCNPGsValidatingWebhookConf(
	ctx context.Context,
	crudClient client.Client,
) (
	*admissionregistrationv1.ValidatingWebhookConfiguration, error,
) {
	validatingWebhookConf := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err := crudClient.Get(ctx, types.NamespacedName{Name: controller.ValidatingWebhookConfigurationName},
		validatingWebhookConf)
	return validatingWebhookConf, err
}

// GetValidatingWebhookByName get ValidatingWebhook by the name of one
// of the webhooks
func GetValidatingWebhookByName(
	ctx context.Context,
	crudClient client.Client,
	name string,
) (
	*admissionregistrationv1.ValidatingWebhookConfiguration, int, error,
) {
	var vWebhooks admissionregistrationv1.ValidatingWebhookConfigurationList
	err := objects.List(ctx, crudClient, &vWebhooks)
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

// UpdateValidatingWebhookConf update the ValidatingWebhook object
func UpdateValidatingWebhookConf(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	wh *admissionregistrationv1.ValidatingWebhookConfiguration,
) error {
	_, err := kubeInterface.AdmissionregistrationV1().
		ValidatingWebhookConfigurations().Update(ctx, wh, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// checkWebhookSetup ensures that the operator has finished the webhook setup.
func checkWebhookSetup(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) error {
	// Check CA
	secret := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      controller.WebhookSecretName,
	}
	err := objects.Get(ctx, crudClient, secretNamespacedName, secret)
	if err != nil {
		return err
	}

	ca := secret.Data["tls.crt"]

	mutatingWebhookConfig, err := getCNPGsMutatingWebhookConf(ctx, crudClient)
	if err != nil {
		return err
	}

	for _, webhook := range mutatingWebhookConfig.Webhooks {
		if !bytes.Equal(webhook.ClientConfig.CABundle, ca) {
			return fmt.Errorf("secret %+v not match with ca bundle in %v: %v is not equal to %v",
				controller.MutatingWebhookConfigurationName, secret, string(ca), string(webhook.ClientConfig.CABundle))
		}
	}

	validatingWebhookConfig, err := getCNPGsValidatingWebhookConf(ctx, crudClient)
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

// getCNPGsMutatingWebhookConf get the MutatingWebhook linked to the operator
func getCNPGsMutatingWebhookConf(
	ctx context.Context,
	crudClient client.Client,
) (
	*admissionregistrationv1.MutatingWebhookConfiguration, error,
) {
	mutatingWebhookConfiguration := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := crudClient.Get(ctx, types.NamespacedName{Name: controller.MutatingWebhookConfigurationName},
		mutatingWebhookConfiguration)
	return mutatingWebhookConfiguration, err
}

// CheckWebhookSetup checks if the webhook denies an invalid request
func isWebhookWorking(
	ctx context.Context,
	crudClient client.Client,
) (bool, error) {
	invalidCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "invalid"},
		Spec:       apiv1.ClusterSpec{Instances: 1},
	}
	_, err := objects.Create(
		ctx,
		crudClient,
		invalidCluster,
		&client.CreateOptions{DryRun: []string{metav1.DryRunAll}},
	)
	// If the error is not an invalid error, return false
	if !errors.IsInvalid(err) {
		return false, fmt.Errorf("expected invalid error, got: %v", err)
	}
	// If the error doesn't contain the expected message, return false
	if !bytes.Contains([]byte(err.Error()), []byte("spec.storage.size")) {
		return false, fmt.Errorf("expected error to contain 'spec.storage.size', got: %v", err)
	}
	return true, nil
}
