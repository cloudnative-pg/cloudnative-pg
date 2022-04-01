/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// CreateClientCertificatesViaKubectlPlugin creates a certificate for a given user on a given cluster
func CreateClientCertificatesViaKubectlPlugin(
	cluster apiv1.Cluster,
	certName string,
	userName string,
	env *TestingEnvironment,
) error {
	// clientCertName := "cluster-cert"
	// user := "app"
	// Create the certificate
	_, _, err := Run(fmt.Sprintf(
		"kubectl cnp certificate %v --cnp-cluster %v --cnp-user %v -n %v",
		certName,
		cluster.Name,
		userName,
		cluster.Namespace))
	if err != nil {
		return err
	}
	// Verifying client certificate secret existence
	secret := &corev1.Secret{}
	err = env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: cluster.Namespace, Name: certName}, secret)
	return err
}
