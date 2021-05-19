/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package utils contains uncategorized utilities only used
// by the instance manager
package utils

import (
	"context"
	"fmt"
	"math"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

var (
	// RetrySettingPrimary is the default retry configuration that is used
	// for promotions
	RetrySettingPrimary = wait.Backoff{
		Duration: 1 * time.Second,
		// Steps is declared as an "int", so we are capping
		// to int32 to support ARM-based 32 bit architectures
		Steps: math.MaxInt32,
	}
)

// ClusterModifier is a function that apply a change
// to a cluster object. This encapsulation is useful to have
// the operator retried when needed
type ClusterModifier func(cluster *apiv1.Cluster) error

// objectToUnstructured converts a runtime Object into an unstructured one
func objectToUnstructured(object runtime.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: data}, nil
}

// ObjectToCluster convert an unstructured object to a Cluster object
func ObjectToCluster(runtimeObject runtime.Object) (*apiv1.Cluster, error) {
	object, err := objectToUnstructured(runtimeObject)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding runtime.Object data from watch: %w",
			err)
	}

	var cluster apiv1.Cluster
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &cluster)
	if err != nil {
		return nil, fmt.Errorf("error decoding Cluster resource: %w", err)
	}

	return &cluster, nil
}

// UpdateClusterStatusAndRetry apply the passed modifier to the passed cluster object and then try to
// store the cluster using the API server. In case of a conflict error the cluster is refreshed from
// the API server and the operation is retried until it will succeed.
func UpdateClusterStatusAndRetry(
	ctx context.Context,
	client ctrl.Client,
	cluster *apiv1.Cluster,
	tx ClusterModifier) (*apiv1.Cluster, error) {
	if err := tx(cluster); err != nil {
		return cluster, err
	}

	clusterName := cluster.Name
	clusterNamespace := cluster.Namespace

	return cluster, retry.RetryOnConflict(RetrySettingPrimary, func() error {
		updateError := client.Status().Update(ctx, cluster)
		if updateError == nil || !apierrors.IsConflict(updateError) {
			return updateError
		}

		log.Log.Info(
			"Conflict detected while changing cluster status, retrying",
			"error", updateError.Error())

		var err error
		cluster = &apiv1.Cluster{}
		err = client.Get(ctx, ctrl.ObjectKey{Namespace: clusterNamespace, Name: clusterName}, cluster)
		if err != nil {
			return err
		}

		err = tx(cluster)
		if err != nil {
			return err
		}

		return updateError
	})
}
