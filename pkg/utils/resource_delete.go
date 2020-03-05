/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	"context"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteObjectIfExists removes an object from the API server, given its
// objectKey and kind info, without getting it first. It the object doesn't
// exist, the error is skipped
func DeleteObjectIfExists(
	ctx context.Context,
	client client.Client,
	objectKey client.ObjectKey,
	gkv schema.GroupVersionKind,
) error {
	u := &unstructured.Unstructured{}
	u.SetName(objectKey.Name)
	u.SetNamespace(objectKey.Namespace)
	u.SetGroupVersionKind(gkv)
	err := client.Delete(ctx, u)

	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// DeleteConfigMapIfExists delete a config map is existent
func DeleteConfigMapIfExists(ctx context.Context, client client.Client, objectKey client.ObjectKey) error {
	return DeleteObjectIfExists(ctx, client, objectKey, schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMap",
	})
}

// DeleteServiceIfExists delete a config map is existent
func DeleteServiceIfExists(ctx context.Context, client client.Client, objectKey client.ObjectKey) error {
	return DeleteObjectIfExists(ctx, client, objectKey, schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Service",
	})
}

// DeleteSecretIfExists delete a secret is existent
func DeleteSecretIfExists(ctx context.Context, client client.Client, objectKey client.ObjectKey) error {
	return DeleteObjectIfExists(ctx, client, objectKey, schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Secret",
	})
}

// DeleteEndpointsIfExists delete an endpoints resource if existent
func DeleteEndpointsIfExists(ctx context.Context, client client.Client, objectKey client.ObjectKey) error {
	return DeleteObjectIfExists(ctx, client, objectKey, schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Endpoints",
	})
}

// DeletePodDisruptionBudgetIfExists delete a PodDisruptionBudget if existent
func DeletePodDisruptionBudgetIfExists(ctx context.Context, client client.Client, objectKey client.ObjectKey) error {
	return DeleteObjectIfExists(ctx, client, objectKey, schema.GroupVersionKind{
		Group:   "policy",
		Version: "v1beta1",
		Kind:    "PodDisruptionBudget",
	})
}
