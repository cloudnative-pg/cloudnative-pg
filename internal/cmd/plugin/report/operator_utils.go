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
	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

func getWebhooks(ctx context.Context, stopRedact bool) (
	*v1.MutatingWebhookConfigurationList, *v1.ValidatingWebhookConfigurationList, error,
) {
	var (
		mutatingWebhookConfigList   v1.MutatingWebhookConfigurationList
		validatingWebhookConfigList v1.ValidatingWebhookConfigurationList
		mWebhookConfig              v1.MutatingWebhookConfigurationList
		vWebhookConfig              v1.ValidatingWebhookConfigurationList
	)

	if err := plugin.Client.List(ctx, &mutatingWebhookConfigList); err != nil {
		return nil, nil, err
	}

	for _, item := range mutatingWebhookConfigList.Items {
		for _, webhook := range item.Webhooks {
			if len(webhook.Rules) > 0 && webhook.Rules[0].APIGroups[0] == apiv1.GroupVersion.Group {
				mWebhookConfig.Items = append(mWebhookConfig.Items, item)
			}
		}
	}
	if !stopRedact {
		for i, item := range mWebhookConfig.Items {
			for j, webhook := range item.Webhooks {
				mWebhookConfig.Items[i].Webhooks[j].ClientConfig = redactWebhookClientConfig(webhook.ClientConfig)
			}
		}
	}

	if err := plugin.Client.List(ctx, &validatingWebhookConfigList); err != nil {
		return nil, nil, err
	}

	for _, item := range validatingWebhookConfigList.Items {
		for _, webhook := range item.Webhooks {
			if len(webhook.Rules) > 0 && webhook.Rules[0].APIGroups[0] == apiv1.GroupVersion.Group {
				vWebhookConfig.Items = append(vWebhookConfig.Items, item)
			}
		}
	}
	if !stopRedact {
		for i, item := range vWebhookConfig.Items {
			for j, webhook := range item.Webhooks {
				vWebhookConfig.Items[i].Webhooks[j].ClientConfig = redactWebhookClientConfig(webhook.ClientConfig)
			}
		}
	}

	if len(mWebhookConfig.Items) == 0 || len(vWebhookConfig.Items) == 0 {
		return nil, nil, fmt.Errorf(
			"can't find the webhooks that targeting resources within the group %s",
			apiv1.GroupVersion.Group,
		)
	}

	return &mWebhookConfig, &vWebhookConfig, nil
}

func getWebhookService(
	ctx context.Context,
	mutatingWebhookList *v1.MutatingWebhookConfigurationList,
) (corev1.Service, error) {
	if len(mutatingWebhookList.Items) == 0 ||
		len(mutatingWebhookList.Items[0].Webhooks) == 0 {
		return corev1.Service{}, nil
	}

	config := mutatingWebhookList.Items[0].Webhooks[0].ClientConfig
	if config.Service == nil {
		return corev1.Service{}, nil
	}
	objKey := types.NamespacedName{
		Name:      config.Service.Name,
		Namespace: config.Service.Namespace,
	}

	var webhookService corev1.Service
	err := plugin.Client.Get(ctx, objKey, &webhookService)

	return webhookService, err
}
