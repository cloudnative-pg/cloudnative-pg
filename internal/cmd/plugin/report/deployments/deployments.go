/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package deployments contains code to get operator deployment
package deployments

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

// GetOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
func GetOperatorDeployment(ctx context.Context) (appsv1.Deployment, error) {
	const operatorDeploymentName = "postgresql-operator-controller-manager"
	deploymentList := &appsv1.DeploymentList{}

	if err := plugin.Client.List(
		ctx, deploymentList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"},
	); err != nil {
		return appsv1.Deployment{}, err
	}
	// We check if we have one or more deployments
	switch {
	case len(deploymentList.Items) > 1:
		err := fmt.Errorf("number of operator deployments != 1")
		return appsv1.Deployment{}, err
	case len(deploymentList.Items) == 1:
		return deploymentList.Items[0], nil
	}

	if err := plugin.Client.List(
		ctx,
		deploymentList,
		ctrlclient.HasLabels{"operators.coreos.com/cloud-native-postgresql.openshift-operators"},
	); err != nil {
		return appsv1.Deployment{}, err
	}

	// We check if we have one or more deployments
	switch {
	case len(deploymentList.Items) > 1:
		err := fmt.Errorf("number of operator deployments != 1")
		return appsv1.Deployment{}, err
	case len(deploymentList.Items) == 1:
		return deploymentList.Items[0], nil
	}

	// This is for deployments created before 1.4.0
	if err := plugin.Client.List(
		ctx, deploymentList, ctrlclient.MatchingFields{"metadata.name": operatorDeploymentName},
	); err != nil {
		return appsv1.Deployment{}, err
	}

	if len(deploymentList.Items) != 1 {
		err := fmt.Errorf("number of %v deployments != 1", operatorDeploymentName)
		return appsv1.Deployment{}, err
	}
	return deploymentList.Items[0], nil
}

// GetOperatorPod returns the operator pod if there is a single one running, error otherwise
func GetOperatorPod(ctx context.Context) (corev1.Pod, error) {
	podList := &corev1.PodList{}

	// This will work for newer version of the operator, which are using
	// our custom label
	if err := plugin.Client.List(
		ctx, podList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"}); err != nil {
		return corev1.Pod{}, err
	}
	switch {
	case len(podList.Items) > 1:
		err := fmt.Errorf("number of running operator pods greater than 1: %v pods running", len(podList.Items))
		return corev1.Pod{}, err

	case len(podList.Items) == 1:
		return podList.Items[0], nil
	}

	operatorNamespace, err := GetOperatorNamespaceName(ctx)
	if err != nil {
		return corev1.Pod{}, err
	}

	// This will work for older version of the operator, which are using
	// the default label from kube-builder
	if err = plugin.Client.List(
		ctx, podList,
		ctrlclient.MatchingLabels{"control-plane": "controller-manager"},
		ctrlclient.InNamespace(operatorNamespace)); err != nil {
		return corev1.Pod{}, err
	}
	if len(podList.Items) != 1 {
		err = fmt.Errorf("number of running operator different than 1: %v pods running", len(podList.Items))
		return corev1.Pod{}, err
	}

	return podList.Items[0], nil
}

// GetOperatorNamespaceName returns the namespace the operator Deployment is running in
func GetOperatorNamespaceName(ctx context.Context) (string, error) {
	deployment, err := GetOperatorDeployment(ctx)
	if err != nil {
		return "", err
	}
	return deployment.GetNamespace(), err
}
