/*
Copyright 2019-2022 The CloudNativePG Contributors

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

	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

func getWebhooks(ctx context.Context, stopRedact bool) (
	*v1.MutatingWebhookConfigurationList, *v1.ValidatingWebhookConfigurationList, error,
) {
	var (
		mutatingWebhookConfigList   v1.MutatingWebhookConfigurationList
		validatingWebhookConfigList v1.ValidatingWebhookConfigurationList
		mWebhookConfig              v1.MutatingWebhookConfigurationList
		vWebhookConfig              v1.ValidatingWebhookConfigurationList
		mutatingWebhookNames        = []string{"mbackup.kb.io", "mcluster.kb.io", "mscheduledbackup.kb.io"}
		validatingWebhookNames      = []string{"vbackup.kb.io", "vcluster.kb.io", "vpooler.kb.io", "vscheduledbackup.kb.io"}
	)

	if err := plugin.Client.List(ctx, &mutatingWebhookConfigList); err != nil {
		return nil, nil, err
	}

	for _, item := range mutatingWebhookConfigList.Items {
		for _, webhook := range item.Webhooks {
			if utils.StringInSlice(mutatingWebhookNames, webhook.Name) {
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
			if utils.StringInSlice(validatingWebhookNames, webhook.Name) {
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
	return &mWebhookConfig, &vWebhookConfig, nil
}

func getWebhookService(ctx context.Context, config v1.WebhookClientConfig) (corev1.Service, error) {
	var webhookService corev1.Service

	objKey := types.NamespacedName{
		Name:      config.Service.Name,
		Namespace: config.Service.Namespace,
	}
	err := plugin.Client.Get(ctx, objKey, &webhookService)

	return webhookService, err
}
