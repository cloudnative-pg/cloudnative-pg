/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// poolerManagedResources contains all the resources that are going to be
// synchronized by the Pooler controller
type poolerManagedResources struct {
	// This is the configmap generated with the pgbouncer configuration
	Configuration *corev1.Secret

	// This is the secret that is being used to authenticate
	// the auth_query connection
	AuthUserSecret *corev1.Secret

	// This is the pgbouncer deployment
	Deployment *appsv1.Deployment

	// This is the service where pgbouncer is accessible
	Service *corev1.Service

	// The referenced Cluster
	Cluster *apiv1.Cluster

	// The RBAC resources needed for the pooler instance manager
	// to watch over the relative Pooler resource
	ServiceAccount *corev1.ServiceAccount
	RoleBinding    *v1.RoleBinding
	Role           *v1.Role
}

// getManagedResources detects the list of the resources created and manager
// by this pooler
func (r *PoolerReconciler) getManagedResources(ctx context.Context,
	pooler *apiv1.Pooler) (result *poolerManagedResources, err error) {
	result = &poolerManagedResources{}

	// Get the config
	result.Configuration, err = getSecretOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	// Get the auth query secret if any
	result.AuthUserSecret, err = getSecretOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.GetAuthQuerySecretName(), Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	// Get the pooler deployment
	result.Deployment, err = getDeploymentOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	// Get the service deployment
	result.Service, err = getServiceOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	// Get the referenced cluster
	result.Cluster, err = getClusterOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Spec.Cluster.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	result.ServiceAccount, err = getServiceAccountOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	result.Role, err = getRoleOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	result.RoleBinding, err = getRoleBindingOrNil(
		ctx, r.Client, client.ObjectKey{Name: pooler.Name, Namespace: pooler.Namespace})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// getDeploymentOrNil gets a deployment with a certain name, returning nil when it doesn't exist
func getDeploymentOrNil(
	ctx context.Context, r client.Client, objectKey client.ObjectKey) (*appsv1.Deployment, error) {
	var deployment appsv1.Deployment
	err := r.Get(ctx, objectKey, &deployment)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &deployment, nil
}

// getServiceOrNil gets a service with a certain name, returning nil when it doesn't exist
func getServiceOrNil(ctx context.Context, r client.Client, objectKey client.ObjectKey) (*corev1.Service, error) {
	var service corev1.Service
	err := r.Get(ctx, objectKey, &service)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &service, nil
}

// getServiceAccountOrNil gets a service account with a certain name, returning nil when it doesn't exist
func getServiceAccountOrNil(
	ctx context.Context,
	r client.Client,
	objectKey client.ObjectKey,
) (*corev1.ServiceAccount, error) {
	var sa corev1.ServiceAccount
	err := r.Get(ctx, objectKey, &sa)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &sa, nil
}

// getRoleOrNil gets a role with a certain name, returning nil when it doesn't exist
func getRoleOrNil(ctx context.Context, r client.Client, objectKey client.ObjectKey) (*v1.Role, error) {
	var role v1.Role
	err := r.Get(ctx, objectKey, &role)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &role, nil
}

// getRoleBindingOrNil gets a rolebinding with a certain name, returning nil when it doesn't exist
func getRoleBindingOrNil(ctx context.Context, r client.Client, objectKey client.ObjectKey) (*v1.RoleBinding, error) {
	var rb v1.RoleBinding
	err := r.Get(ctx, objectKey, &rb)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &rb, nil
}

// getSecretOrNil gets a secret with a certain name, returning nil when it doesn't exist
func getSecretOrNil(ctx context.Context, r client.Client, objectKey client.ObjectKey) (*corev1.Secret, error) {
	var secret corev1.Secret
	err := r.Get(ctx, objectKey, &secret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &secret, nil
}

// getClusterOrNil gets a cluster with a certain name, returning nil when it doesn't exist
func getClusterOrNil(ctx context.Context, r client.Client, objectKey client.ObjectKey) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.Get(ctx, objectKey, &cluster)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &cluster, nil
}
