/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateRoleBinding is the binding between the permissions that PGK can use
// and the ServiceAccount used by the Pod
func CreateRoleBinding(objectMeta metav1.ObjectMeta) rbacv1.RoleBinding {
	return rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: objectMeta.Namespace,
			Name:      objectMeta.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  "",
				Name:      objectMeta.Name,
				Namespace: objectMeta.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     objectMeta.Name,
		},
	}
}
