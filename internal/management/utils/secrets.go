/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
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
