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

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// ServiceAccount creates a service account for a given pooler
func ServiceAccount(pooler *apiv1.Pooler) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name: pooler.GetServiceAccountName(), Namespace: pooler.Namespace,
	}}
}

// Role creates a role for a given pooler
func Role(pooler *apiv1.Pooler) *rbacv1.Role {
	secretNames := []string{pooler.GetAuthQuerySecretName()}
	if pooler.Status.Secrets != nil {
		if pooler.Status.Secrets.ServerCA.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ServerCA.Name)
		}

		if pooler.Status.Secrets.ServerTLS.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ServerTLS.Name)
		}

		if pooler.Status.Secrets.ClientCA.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ClientCA.Name)
		}

		if pooler.Status.Secrets.ClientTLS.Name != "" {
			secretNames = append(secretNames, pooler.Status.Secrets.ClientTLS.Name)
		}
	}

	return &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{
		Name: pooler.Name, Namespace: pooler.Namespace,
	}, Rules: []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"poolers",
			},
			Verbs: []string{
				"get",
				"watch",
			},
			ResourceNames: []string{
				pooler.Name,
			},
		},
		{
			APIGroups: []string{
				"postgresql.cnpg.io",
			},
			Resources: []string{
				"poolers/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
				"watch",
			},
			ResourceNames: []string{
				pooler.Name,
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"secrets",
			},
			Verbs: []string{
				"get",
				"watch",
			},
			ResourceNames: secretNames,
		},
	}}
}

// RoleBinding creates a role binding for a given pooler
func RoleBinding(pooler *apiv1.Pooler, serviceAccount string) rbacv1.RoleBinding {
	return specs.CreateRoleBinding(pooler.ObjectMeta, serviceAccount)
}
