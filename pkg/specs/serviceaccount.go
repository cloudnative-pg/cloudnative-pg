/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"encoding/json"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// OperatorManagedSecretsName is the name of the annotation containing the secrets
	// managed by the operator inside the generated service account
	OperatorManagedSecretsName = "k8s.enterprisedb.io/managedSecrets" // #nosec
)

// CreateServiceAccount create the ServiceAccount that will be used in every Pod
func CreateServiceAccount(cluster metav1.ObjectMeta, imagePullSecretsNames []string) (*corev1.ServiceAccount, error) {
	imagePullSecrets := make([]corev1.LocalObjectReference, len(imagePullSecretsNames))

	for idx, name := range imagePullSecretsNames {
		imagePullSecrets[idx] = corev1.LocalObjectReference{Name: name}
	}

	annotationValue, err := CreateManagedSecretsAnnotationValue(imagePullSecretsNames)
	if err != nil {
		return nil, err
	}

	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Annotations: map[string]string{
				OperatorManagedSecretsName: annotationValue,
			},
		},
		ImagePullSecrets: imagePullSecrets,
	}

	return &serviceAccount, nil
}

// CreateManagedSecretsAnnotationValue create the value of the annotations that stores
// the names of the secrets managed by the operator inside a ServiceAccount
func CreateManagedSecretsAnnotationValue(imagePullSecretsNames []string) (string, error) {
	result, err := json.Marshal(imagePullSecretsNames)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// IsServiceAccountAligned compare the given list of pull secrets with the
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
