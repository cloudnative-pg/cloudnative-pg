/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
