/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package external

import (
	"context"
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external/internal/pgpass"
)

const (
	// defaultExternalSecretsPath is the default path where the cryptographic material
	// needed to connect to an external cluster will be dumped
	defaultExternalSecretsPath = "/controller/external" // #nosec
)

// CustomExternalSecretsPath is the custom path where the cryptographic material
// needed to connect to an external cluster will be dumped.
// This will be used by the unit tests.
var customExternalSecretsPath string

func getExternalSecretsPath() string {
	if customExternalSecretsPath != "" {
		return customExternalSecretsPath
	}

	return defaultExternalSecretsPath
}

// readSecretKeyRef reads the passed secret selector into a string.
// This function is mainly useful to get a PostgreSQL's role password
// from a Kubernetes secret
func readSecretKeyRef(
	ctx context.Context, client ctrl.Client,
	namespace string, selector *corev1.SecretKeySelector,
) (string, error) {
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

// dumpSecretKeyRefToFile dumps a certain secret to a file inside a temporary folder
// using 0600 as permission bits.
//
// This function overlaps with the Kubernetes ability to mount a secret in a pod,
// with the difference that we can change the attached secret without restarting the pod.
// We also need to have more control over when the secret content is updated.
func dumpSecretKeyRefToFile(
	ctx context.Context, client ctrl.Client,
	namespace string, serverName string, selector *corev1.SecretKeySelector,
) (string, error) {
	var secret corev1.Secret

	err := client.Get(ctx, ctrl.ObjectKey{Namespace: namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	directory := path.Join(getExternalSecretsPath(), serverName)
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

// createPgPassFile creates a pgpass file inside the user home directory
func createPgPassFile(
	serverName string,
	connectionParameters map[string]string,
	password string,
) (string, error) {
	pgpassLine := pgpass.NewConnectionInfo(connectionParameters, password)

	directory := path.Join(getExternalSecretsPath(), serverName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	filePath := path.Join(directory, "pgpass")

	return filePath, pgpass.From(pgpassLine).Write(filePath)
}
