/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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

// SetAsOwnedByOperatorDeployment sets the controlled object as owned by the operator deployment.
//
// IMPORTANT: The controlled resource must reside in the same namespace as the operator as described by:
// https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
func SetAsOwnedByOperatorDeployment(ctx context.Context,
	client kubernetes.Interface,
	controlled *metav1.ObjectMeta,
) error {
	const operatorDepName = "postgresql-operator-controller-manager"
	// This is the easiest way to get the typeMeta.
	// If you instantiate an empty v1.Deployment struct it does NOT contain the typeMeta information.
	typeMeta := metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}

	getOptions := metav1.GetOptions{TypeMeta: typeMeta}
	dep, err := client.AppsV1().Deployments(controlled.Namespace).Get(ctx, operatorDepName, getOptions)
	if err != nil {
		return err
	}

	// The deployment typeMeta is empty (kubernetes bug), so we pass the one we already populated.
	SetAsOwnedBy(controlled, dep.ObjectMeta, typeMeta)

	return nil
}
