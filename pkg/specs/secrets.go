/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		},
		Type: corev1.SecretTypeBasicAuth,
		StringData: map[string]string{
			"username": username,
			"password": password,
			"pgpass": fmt.Sprintf(
				"%v:%v:%v:%v:%v\n",
				hostname,
				5432,
				dbname,
				username,
				password),
		},
	}
}
