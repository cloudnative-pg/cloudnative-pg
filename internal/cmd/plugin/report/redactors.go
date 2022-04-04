/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
)

// redactSecret creates a version of a Secret with only the Data map's KEYS
func redactSecret(secret corev1.Secret) corev1.Secret {
	redacted := secret
	redacted.Data = make(map[string][]byte)
	for k := range secret.Data {
		redacted.Data[k] = []byte("")
	}
	return redacted
}

// passSecret passes an unmodified Secret
func passSecret(secret corev1.Secret) corev1.Secret {
	return secret
}

// redactConfigMap creates a version of a ConfigMap with only the Data map's KEYS
func redactConfigMap(configMap corev1.ConfigMap) corev1.ConfigMap {
	redacted := configMap
	redacted.Data = make(map[string]string)
	for k := range configMap.Data {
		redacted.Data[k] = ""
	}
	return redacted
}

// passConfigMap passes an unmodified ConfigMap
func passConfigMap(configMap corev1.ConfigMap) corev1.ConfigMap {
	return configMap
}

// redactWebhookClientConfig makes a copy of a WebhookClientConfig with the CA obfuscated
//
// Useful to redact validating/mutating webhook configurations
//
// WARN: the CABundle in the WebhookClientConfig is optional, and if blank / missing,
// the trust roots on API server are used. So, in this case, leaving the secret blank
// may change the behavior. See https://pkg.go.dev/k8s.io/api@v0.23.5/admissionregistration/v1#WebhookClientConfig
// If the CABundle is present, override with a small string. The one chosen is "-"
// which will print in Base64 as "LQ=="
func redactWebhookClientConfig(config admissionv1.WebhookClientConfig) admissionv1.WebhookClientConfig {
	if len(config.CABundle) != 0 {
		config.CABundle = []byte("-") // will print in Base64 as "LQ=="
	}
	return config
}
