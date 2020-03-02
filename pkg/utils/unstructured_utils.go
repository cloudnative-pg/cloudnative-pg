/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetCurrentPrimary get the current primary name from a cluster object
func GetCurrentPrimary(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "status", "currentPrimary")
	if err != nil {
		return "", err
	}

	if !found {
		return "", nil
	}

	return result, nil
}

// GetTargetPrimary get the current primary name from a cluster object
func GetTargetPrimary(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "status", "targetPrimary")
	if err != nil {
		return "", err
	}

	if !found {
		return "", nil
	}

	return result, nil
}

// SetCurrentPrimary set the current primary name in the cluster object
func SetCurrentPrimary(data *unstructured.Unstructured, name string) error {
	return unstructured.SetNestedField(data.Object, name, "status", "currentPrimary")
}

// SetTargetPrimary set the current primary name in the cluster object
func SetTargetPrimary(data *unstructured.Unstructured, name string) error {
	return unstructured.SetNestedField(data.Object, name, "status", "targetPrimary")
}
