/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/gomega" //nolint

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

// GetPGBouncerPodList gathers the pgbouncer pod list
func GetPGBouncerPodList(namespace, poolerYamlFilePath string, env *TestingEnvironment) (*corev1.PodList, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
	if err != nil {
		return &corev1.PodList{}, err
	}
	return podList, err
}

// GetPGBouncerDeployment gathers the pgbouncer deployment info
func GetPGBouncerDeployment(
	namespace,
	poolerYamlFilePath string,
	env *TestingEnvironment) (*appsv1.Deployment, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	// Wait for the deployment to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      poolerName,
	}
	deployment := &appsv1.Deployment{}
	err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)

	if err != nil {
		return &appsv1.Deployment{}, err
	}

	return deployment, nil
}

// GetPoolerEndpoints retrieves the pooler endpoints
func GetPoolerEndpoints(
	namespace,
	poolerYamlFilePath string,
	env *TestingEnvironment) (*corev1.Endpoints, error) {
	endPoint := &corev1.Endpoints{}
	endPointName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	// Wait for the deployment to be ready
	endPointNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      endPointName,
	}
	err = env.Client.Get(env.Ctx, endPointNamespacedName, endPoint)
	if err != nil {
		return &corev1.Endpoints{}, err
	}

	return endPoint, nil
}
