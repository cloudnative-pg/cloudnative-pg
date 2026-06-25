/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
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

// ErrInvalidPathComponent is returned when a value that is used as a filesystem
// path component contains a path separator or a parent-directory reference, both
// of which could be used to escape the external secrets directory.
var ErrInvalidPathComponent = errors.New("value cannot be used as a filesystem path component")

// validatePathComponent ensures that a spec-provided value can be safely joined
// into the external secrets path without escaping it. The admission webhook
// rejects these values too, but the instance manager reads the spec directly, so
// the check is repeated here as a defense-in-depth guard.
func validatePathComponent(value string) error {
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%w: %q", ErrInvalidPathComponent, value)
	}
	return nil
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

// getSecretKeyRefFileName get the name of the file where the content of the
// connection secret will be dumped
func getSecretKeyRefFileName(
	serverName string,
	selector *corev1.SecretKeySelector,
) string {
	directory := filepath.Join(getExternalSecretsPath(), serverName)
	filePath := filepath.Join(directory, fmt.Sprintf("%v_%v", selector.Name, selector.Key))
	return filePath
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
	for _, component := range []string{serverName, selector.Name, selector.Key} {
		if err := validatePathComponent(component); err != nil {
			return "", err
		}
	}

	var secret corev1.Secret

	err := client.Get(ctx, ctrl.ObjectKey{Namespace: namespace, Name: selector.Name}, &secret)
	if err != nil {
		return "", err
	}

	value, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("missing key %v in secret %v", selector.Key, selector.Name)
	}

	directory := filepath.Join(getExternalSecretsPath(), serverName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}

	filePath := filepath.Join(directory, fmt.Sprintf("%v_%v", selector.Name, selector.Key))
	// Write atomically: this file is reused across reconciliations and read
	// concurrently by libpq, so an in-place rewrite could expose a partial file
	// or leave stale bytes behind when the secret rotates to shorter content.
	if _, err := fileutils.WriteFileAtomic(filePath, value, 0o600); err != nil {
		return "", err
	}

	return filePath, nil
}

// getPgPassFilePath gets the path where the pgpass file will be stored
func getPgPassFilePath(serverName string) string {
	directory := filepath.Join(getExternalSecretsPath(), serverName)
	filePath := filepath.Join(directory, "pgpass")
	return filePath
}

// createPgPassFile creates a pgpass file inside the user home directory
func createPgPassFile(
	serverName string,
	connectionParameters map[string]string,
	password string,
) (string, error) {
	pgpassLine := pgpass.NewConnectionInfo(connectionParameters, password)

	if err := validatePathComponent(serverName); err != nil {
		return "", err
	}

	directory := filepath.Join(getExternalSecretsPath(), serverName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	filePath := filepath.Join(directory, "pgpass")

	return filePath, pgpass.From(pgpassLine).Write(filePath)
}
