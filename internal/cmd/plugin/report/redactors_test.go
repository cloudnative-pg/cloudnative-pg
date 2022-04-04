/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Redact Secret", func() {
	It("should properly works", func() {
		data := make(map[string][]byte, 1)
		data["test"] = []byte("Secret")
		secret := corev1.Secret{Data: data}
		redactedSecret := redactSecret(secret)

		Expect(redactedSecret).ToNot(BeEquivalentTo(secret))
		Expect(redactedSecret.Data["test"]).Should(BeEquivalentTo([]byte("")))
	})
})

var _ = Describe("Redact ConfigMap", func() {
	It("should properly works", func() {
		data := make(map[string]string, 1)
		data["test"] = "ConfigMap"
		configMap := corev1.ConfigMap{Data: data}
		redactedConfigMap := redactConfigMap(configMap)

		Expect(redactedConfigMap).ToNot(BeEquivalentTo(configMap))
		Expect(redactedConfigMap.Data["test"]).Should(BeEquivalentTo([]byte("")))
	})
})

var _ = Describe("Redact WebhookClientConfig", func() {
	It("should override CABundle if present", func() {
		webhookClientConfig := admissionv1.WebhookClientConfig{CABundle: []byte("test")}
		redactedWebhookClientConfig := redactWebhookClientConfig(webhookClientConfig)
		Expect(redactedWebhookClientConfig).ToNot(BeEquivalentTo(webhookClientConfig))
		Expect(redactedWebhookClientConfig.CABundle).Should(BeEquivalentTo([]byte("-")))
	})

	It("should not create CABundle if missing", func() {
		webhookClientConfig := admissionv1.WebhookClientConfig{}
		redactedWebhookClientConfig := redactWebhookClientConfig(webhookClientConfig)
		Expect(redactedWebhookClientConfig).To(BeEquivalentTo(webhookClientConfig))
		Expect(redactedWebhookClientConfig.CABundle).Should(BeEmpty())
	})
})
