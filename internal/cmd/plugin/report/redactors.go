/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package report

import corev1 "k8s.io/api/core/v1"

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
