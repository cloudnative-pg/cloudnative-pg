/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"context"
	"fmt"

	v1 "k8s.io/api/apps/v1"

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
	operatorLabelSelector string,
) error {
	deployment, err := GetOperatorDeployment(ctx, client, controlled.Namespace, operatorLabelSelector)
	if err != nil {
		return err
	}

	// The deployment typeMeta is empty (kubernetes bug), so we need to explicitly populate it.
	typeMeta := metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	SetAsOwnedBy(controlled, deployment.ObjectMeta, typeMeta)

	return nil
}

// GetOperatorDeployment find the operator deployment using labels
// and then return the deployment object, in case we can't find a deployment
// or we find more than one, we just return an error.
func GetOperatorDeployment(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, operatorLabelSelector string) (*v1.Deployment, error) {
	deploymentList, err := client.AppsV1().Deployments(namespace).List(
		ctx, metav1.ListOptions{LabelSelector: operatorLabelSelector})
	if err != nil {
		return nil, err
	}
	switch {
	case len(deploymentList.Items) == 1:
		return &deploymentList.Items[0], nil
	case len(deploymentList.Items) > 1:
		return nil, fmt.Errorf("more than one operator deployment running")
	}

	deploymentList, err = client.AppsV1().Deployments(namespace).List(
		ctx, metav1.ListOptions{LabelSelector: "operators.coreos.com/cloud-native-postgresql.openshift-operators="})
	if err != nil {
		return nil, err
	}

	switch {
	case len(deploymentList.Items) == 0:
		return nil, fmt.Errorf("no deployment detected")
	case len(deploymentList.Items) > 1:
		return nil, fmt.Errorf("more than one operator deployment running")
	}

	return &deploymentList.Items[0], nil
}
