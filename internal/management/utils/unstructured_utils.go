/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package utils contains uncategorized utilities only used
// by the instance manager
package utils

import (
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
)

var (
	// ErrCurrentPrimaryNotFound means that we can't find which server is the primary
	// one in the cluster status
	ErrCurrentPrimaryNotFound = errors.New("current primary not found")

	// ErrTargetPrimaryNotFound means that we can't find which server is the target
	// one in the cluster status
	ErrTargetPrimaryNotFound = errors.New("target primary not found")

	// ErrInstancesNotFound means that we can't find the required instance number
	ErrInstancesNotFound = errors.New("instances not found")

	// ErrPostgreSQLConfigurationMissing means that the ConfigMap does not contain
	// the PostgreSQL configuration of this cluster
	ErrPostgreSQLConfigurationMissing = errors.New("missing postgresConfiguration in ConfigMap")

	// ErrPostgreSQLHBAMissing means that the ConfigMap does not contain
	// the PostgreSQL HBA rules of this cluster
	ErrPostgreSQLHBAMissing = errors.New("missing postgresHBA in ConfigMap")

	// ErrCertificateMissing means that the tls.crt file is missing in
	// a TLS secret
	ErrCertificateMissing = errors.New("missing tls.crt data in secret")

	// ErrCACertificateMissing means that the tls.crt file is missing in
	// a TLS secret
	ErrCACertificateMissing = errors.New("missing ca.crt data in secret")

	// ErrPrivateKeyMissing means that the tls.key file is missing in
	// a TLS secret
	ErrPrivateKeyMissing = errors.New("missing tls.key data in secret")

	// ErrInvalidObject means that the metadata of the passed object are
	// missing or invalid
	ErrInvalidObject = errors.New("missing object metadata")
)

// GetNamespace get the namespace of an object
func GetNamespace(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "metadata", "namespace")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrInvalidObject
	}

	return result, nil
}

// GetName get the name of an object
func GetName(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "metadata", "name")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrInvalidObject
	}

	return result, nil
}

// GetCurrentPrimary get the current primary name from a cluster object
func GetCurrentPrimary(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "status", "currentPrimary")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrCurrentPrimaryNotFound
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
		return "", ErrTargetPrimaryNotFound
	}

	return result, nil
}

// GetInstances get the current number of instances
func GetInstances(data *unstructured.Unstructured) (int64, error) {
	result, found, err := unstructured.NestedInt64(data.Object, "spec", "instances")
	if err != nil {
		return 0, err
	}

	if !found {
		return 0, ErrInstancesNotFound
	}

	return result, nil
}

// GetPostgreSQLConfiguration get the current PostgreSQL configuration
// from the ConfigMag generated for this cluster
func GetPostgreSQLConfiguration(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "data", specs.PostgreSQLConfigurationKeyName)
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrPostgreSQLConfigurationMissing
	}

	return result, nil
}

// GetPostgreSQLHBA get the current PostgreSQL HBA configuration
// from the ConfigMap generated for this cluster
func GetPostgreSQLHBA(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "data", specs.PostgreSQLHBAKeyName)
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrPostgreSQLHBAMissing
	}

	return result, nil
}

// GetCertificate get the tls certificate from a secret of TLS type
func GetCertificate(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "data", "tls.crt")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrCertificateMissing
	}

	return result, nil
}

// GetPrivateKey get the tls private key from a secret of TLS type
func GetPrivateKey(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "data", "tls.key")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrPrivateKeyMissing
	}

	return result, nil
}

// GetCACertificate get the tls certificate from a secret of TLS type
func GetCACertificate(data *unstructured.Unstructured) (string, error) {
	result, found, err := unstructured.NestedString(data.Object, "data", "ca.crt")
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrCACertificateMissing
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
