/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	. "github.com/onsi/gomega" // nolint
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
)

// CreateSecretCA generates a CA for the cluster and return the cluster and the key pair
func CreateSecretCA(
	namespace string,
	clusterName string,
	caSecName string,
	includeCAPrivateKey bool,
	env *TestingEnvironment) (
	*apiv1.Cluster, *certs.KeyPair) {
	// creating root CA certificates
	cluster := &apiv1.Cluster{}
	cluster.Namespace = namespace
	cluster.Name = clusterName
	secret := &corev1.Secret{}
	err := env.Client.Get(env.Ctx, client.ObjectKey{Namespace: namespace, Name: caSecName}, secret)
	Expect(apierrors.IsNotFound(err)).To(BeTrue())

	caPair, err := certs.CreateRootCA(cluster.Name, namespace)
	Expect(err).ToNot(HaveOccurred())

	caSecret := caPair.GenerateCASecret(namespace, caSecName)
	// delete the key from the CA, as it is not needed in this case
	if !includeCAPrivateKey {
		delete(caSecret.Data, certs.CAPrivateKeyKey)
	}
	err = env.Client.Create(env.Ctx, caSecret)
	Expect(err).ToNot(HaveOccurred())
	return cluster, caPair
}
