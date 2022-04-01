/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package specs

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
)

// CreateSecret create a secret with the PostgreSQL and the owner passwords
func CreateSecret(
	name string,
	namespace string,
	hostname string,
	dbname string,
	username string,
	password string,
) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				WatchedLabelName: "true",
			},
		},
		Type: corev1.SecretTypeBasicAuth,
		StringData: map[string]string{
			"username": username,
			"password": password,
			"pgpass": fmt.Sprintf(
				"%v:%v:%v:%v:%v\n",
				hostname,
				postgres.ServerPort,
				dbname,
				username,
				password),
		},
	}
}
