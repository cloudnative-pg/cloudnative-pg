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
