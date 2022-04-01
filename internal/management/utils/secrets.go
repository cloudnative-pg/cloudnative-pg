/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package utils contains uncategorized utilities only used
// by the instance manager of PostgreSQL and PgBouncer
package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// GetUserPasswordFromSecret gets the username and the password from
// a secret of type basic-auth
func GetUserPasswordFromSecret(secret *corev1.Secret) (string, string, error) {
	if _, ok := secret.Data["username"]; !ok {
		return "", "", fmt.Errorf("username key doesn't exist inside the secret")
	}

	if _, ok := secret.Data["password"]; !ok {
		return "", "", fmt.Errorf("password key doesn't exist inside the secret")
	}

	return string(secret.Data["username"]), string(secret.Data["password"]), nil
}
