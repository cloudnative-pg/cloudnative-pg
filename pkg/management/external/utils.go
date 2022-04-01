/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package external

import (
	"context"
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// externalSecretsPath is the path where the cryptographic material
	// needed to connect to an external cluster will be dumped
	externalSecretsPath = "/controller/external" // #nosec
)

// ReadSecretKeyRef dumps a certain secret to a file inside a temporary folder
// using 0600 as permission bits
func ReadSecretKeyRef(
	ctx context.Context, client ctrl.Client,
	namespace string, selector *corev1.SecretKeySelector) (string, error) {
	var secret corev1.Secret
	err := client.Get(ctx, ctrl.ObjectKey{Namespace: namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	return string(value), err
}

// DumpSecretKeyRefToFile dumps a certain secret to a file inside a temporary folder
// using 0600 as permission bits.
//
// This function overlaps with the Kubernetes ability to mount a secret in a pod,
// with the difference that we can change the attached secret without restarting the pod.
// We also need to have more control over when the secret content is updated.
func DumpSecretKeyRefToFile(
	ctx context.Context, client ctrl.Client,
	namespace string, serverName string, selector *corev1.SecretKeySelector) (string, error) {
	var secret corev1.Secret

	err := client.Get(ctx, ctrl.ObjectKey{Namespace: namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	directory := path.Join(externalSecretsPath, serverName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}

	filePath := path.Join(directory, fmt.Sprintf("%v_%v", selector.Name, selector.Key))
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0o600) // #nosec
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.Write(value)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}

// CreatePgPassFile creates a pgpass file inside the user home directory
func CreatePgPassFile(serverName, password string) (string, error) {
	pgpassContent := fmt.Sprintf(
		"%v:%v:%v:%v:%v",
		"*", // hostname
		"*", // port
		"*", // database
		"*", // username
		password)

	directory := path.Join(externalSecretsPath, serverName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}

	filePath := path.Join(directory, "pgpass")
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0o600) // #nosec
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.WriteString(pgpassContent)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}
