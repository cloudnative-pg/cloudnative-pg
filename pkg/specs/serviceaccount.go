/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"encoding/json"
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

const (
	// OperatorManagedSecretsName is the name of the annotation containing the secrets
	// managed by the operator inside the generated service account
	OperatorManagedSecretsName = "k8s.enterprisedb.io/managedSecrets" // #nosec
)

// UpdateServiceAccount sets the needed values in the ServiceAccount that will be used in every Pod
func UpdateServiceAccount(imagePullSecretsNames []string, serviceAccount *corev1.ServiceAccount) error {
	if serviceAccount.ImagePullSecrets == nil {
		serviceAccount.ImagePullSecrets = []corev1.LocalObjectReference{}
	}

	var newReferences []corev1.LocalObjectReference
	for _, name := range imagePullSecretsNames {
		found := false
		for _, existing := range serviceAccount.ImagePullSecrets {
			if name == existing.Name {
				found = true
				break
			}
		}
		if !found {
			newReferences = append(newReferences, corev1.LocalObjectReference{Name: name})
		}
	}
	serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, newReferences...)

	annotationValue, err := CreateManagedSecretsAnnotationValue(imagePullSecretsNames)
	if err != nil {
		return err
	}

	if serviceAccount.Annotations == nil {
		serviceAccount.Annotations = map[string]string{}
	}
	serviceAccount.Annotations[OperatorManagedSecretsName] = annotationValue

	return nil
}

// CreateManagedSecretsAnnotationValue creates the value of the annotations that stores
// the names of the secrets managed by the operator inside a ServiceAccount
func CreateManagedSecretsAnnotationValue(imagePullSecretsNames []string) (string, error) {
	result, err := json.Marshal(imagePullSecretsNames)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// IsServiceAccountAligned compares the given list of pull secrets with the
// ones managed by the operator inside the given ServiceAccount and returns
// true when everything is aligned
func IsServiceAccountAligned(sa *corev1.ServiceAccount, imagePullSecretsNames []string) (bool, error) {
	// This is an old version of the ServiceAccount, that need to be refreshed to
	// store the annotation value
	if sa.Annotations == nil {
		return false, nil
	}

	value := sa.Annotations[OperatorManagedSecretsName]
	if value == "" {
		return false, nil
	}

	var serviceAccountPullSecrets []string
	if err := json.Unmarshal([]byte(value), &serviceAccountPullSecrets); err != nil {
		return false, err
	}

	return reflect.DeepEqual(serviceAccountPullSecrets, imagePullSecretsNames), nil
}
