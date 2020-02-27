/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// getCurrentPrimary get the current primary name from a cluster object
func getCurrentPrimary(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "status", "currentPrimary")
	if err != nil {
		return "", err
	}

	if !found {
		return "", nil
	}

	return result, nil
}

// getTargetPrimary get the current primary name from a cluster object
func getTargetPrimary(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "status", "targetPrimary")
	if err != nil {
		return "", err
	}

	if !found {
		return "", nil
	}

	return result, nil
}

// setCurrentPrimary set the current primary name in the cluster object
func setCurrentPrimary(data *unstructured.Unstructured, name string) error {
	return unstructured.SetNestedField(data.Object, name, "status", "currentPrimary")
}
