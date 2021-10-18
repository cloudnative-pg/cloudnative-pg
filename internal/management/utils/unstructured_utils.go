/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package utils contains uncategorized utilities only used
// by the instance manager
package utils

import (
	"fmt"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// RetrySettingPrimary is the default retry configuration that is used
// for promotions
var RetrySettingPrimary = wait.Backoff{
	Duration: 1 * time.Second,
	// Steps is declared as an "int", so we are capping
	// to int32 to support ARM-based 32 bit architectures
	Steps: math.MaxInt32,
}

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

// ObjectToPooler convert an unstructured object to a Pooler object
func ObjectToPooler(runtimeObject runtime.Object) (*apiv1.Pooler, error) {
	object, err := objectToUnstructured(runtimeObject)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding runtime.Object data from watch: %w",
			err)
	}

	var pooler apiv1.Pooler
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &pooler)
	if err != nil {
		return nil, fmt.Errorf("error decoding Cluster resource: %w", err)
	}

	return &pooler, nil
}
