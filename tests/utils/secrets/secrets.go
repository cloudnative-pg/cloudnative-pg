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

// Package secrets provides functions to manage and handle secrets
package secrets

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// CreateSecretCA generates a CA for the cluster and return the cluster and the key pair
func CreateSecretCA(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName, caSecName string,
	includeCAPrivateKey bool,
) (
	*apiv1.Cluster, *certs.KeyPair, error,
) {
	// creating root CA certificates
	cluster := &apiv1.Cluster{}
	cluster.Namespace = namespace
	cluster.Name = clusterName
	secret := &corev1.Secret{}
	err := crudClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: caSecName}, secret)
	if !apierrors.IsNotFound(err) {
		return cluster, nil, err
	}

	caPair, err := certs.CreateRootCA(cluster.Name, namespace)
	if err != nil {
		return cluster, nil, err
	}

	caSecret := caPair.GenerateCASecret(namespace, caSecName)
	// delete the key from the CA, as it is not needed in this case
	if !includeCAPrivateKey {
		delete(caSecret.Data, certs.CAPrivateKeyKey)
	}
	_, err = objects.Create(ctx, crudClient, caSecret)
	if err != nil {
		return cluster, caPair, err
	}
	return cluster, caPair, nil
}

// GetCredentials retrieve username and password from secrets and return it as per user suffix
func GetCredentials(
	ctx context.Context,
	crudClient client.Client,
	clusterName, namespace, secretSuffix string,
) (
	string, string, error,
) {
	// Get the cluster
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return "", "", err
	}

	var secretName string
	switch secretSuffix {
	case apiv1.SuperUserSecretSuffix:
		secretName = cluster.GetSuperuserSecretName()
	case apiv1.ApplicationUserSecretSuffix:
		secretName = cluster.GetApplicationSecretName()
	default:
		return "", "", fmt.Errorf("unexpected secretSuffix %s", secretSuffix)
	}

	// Get the password as per user suffix in secret
	secret := &corev1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	err = crudClient.Get(ctx, secretNamespacedName, secret)
	if err != nil {
		return "", "", err
	}
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	return username, password, nil
}

// CreateObjectStorageSecret generates an Opaque Secret with a given ID and Key
func CreateObjectStorageSecret(
	ctx context.Context,
	crudClient client.Client,
	namespace, secretName string,
	id, key string,
) (*corev1.Secret, error) {
	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		StringData: map[string]string{
			"ID":  id,
			"KEY": key,
		},
		Type: corev1.SecretTypeOpaque,
	}
	obj, err := objects.Create(ctx, crudClient, targetSecret)
	if err != nil {
		return nil, err
	}

	return obj.(*corev1.Secret), nil
}
