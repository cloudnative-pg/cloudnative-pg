/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SetAsOwnedBy sets the controlled object as owned by a certain other
// controller object with his type information
func SetAsOwnedBy(controlled *metav1.ObjectMeta, controller metav1.ObjectMeta, typeMeta metav1.TypeMeta) {
	isController := true

	controlled.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: typeMeta.APIVersion,
			Kind:       typeMeta.Kind,
			Name:       controller.Name,
			UID:        controller.UID,
			Controller: &isController,
		},
	})
}
