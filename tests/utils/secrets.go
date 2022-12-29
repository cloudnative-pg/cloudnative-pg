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

package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

// CreateSecretCA generates a CA for the cluster and return the cluster and the key pair
func CreateSecretCA(
	namespace string,
	clusterName string,
	caSecName string,
	includeCAPrivateKey bool,
	env *TestingEnvironment) (
	*apiv1.Cluster, *certs.KeyPair, error,
) {
	// creating root CA certificates
	cluster := &apiv1.Cluster{}
	cluster.Namespace = namespace
	cluster.Name = clusterName
	secret := &corev1.Secret{}
	err := env.Client.Get(env.Ctx, client.ObjectKey{Namespace: namespace, Name: caSecName}, secret)
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
	err = CreateObject(env, caSecret)
	if err != nil {
		return cluster, caPair, err
	}
	return cluster, caPair, nil
}

// GetCredentials retrieve username and password from secrets and return it as per user suffix
func GetCredentials(
	clusterName, namespace string,
	secretSuffix string,
	env *TestingEnvironment) (
	string, string, error,
) {
	// Get the cluster
	cluster, err := env.GetCluster(namespace, clusterName)
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
	err = env.Client.Get(env.Ctx, secretNamespacedName, secret)
	if err != nil {
		return "", "", err
	}
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	return username, password, nil
}
