/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"context"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	v1 "k8s.io/api/admissionregistration/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
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

func getWebhookService(ctx context.Context, config v1.WebhookClientConfig) (v12.Service, error) {
	var webhookService v12.Service

	objKey := types.NamespacedName{
		Name:      config.Service.Name,
		Namespace: config.Service.Namespace,
	}
	err := plugin.Client.Get(ctx, objKey, &webhookService)

	return webhookService, err
}
