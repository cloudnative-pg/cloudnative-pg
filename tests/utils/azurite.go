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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateCertificateSecretsOnAzurite will create secrets for Azurite deployment
func CreateCertificateSecretsOnAzurite(
	namespace,
	clusterName,
	azuriteCaSecName,
	azuriteTLSSecName string,
	env *TestingEnvironment,
) {
	ginkgo.By("creating ca and tls certificate secrets", func() {
		// create CA certificates
		_, caPair, err := CreateSecretCA(namespace, clusterName, azuriteCaSecName, true, env)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// sign and create secret using CA certificate and key
		serverPair, err := caPair.CreateAndSignPair("azurite", certs.CertTypeServer,
			[]string{"azurite.internal.mydomain.net, azurite.default.svc, azurite.default,"},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		serverSecret := serverPair.GenerateCertificateSecret(namespace, azuriteTLSSecName)
		err = env.Client.Create(env.Ctx, serverSecret)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
}

// CreateStorageCredentialsOnAzurite will create credentials for Azurite
func CreateStorageCredentialsOnAzurite(namespace string) error {
	secretFile := "../e2e/fixtures/backup/azurite/azurite-secret.yaml" // nolint
	_, _, err := Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, secretFile))
	return err
}

// InstallAzurite will setup Azurite in defined nameSpace and creates service
func InstallAzurite(namespace string, env *TestingEnvironment) error {
	azuriteDeploymentFile := "../e2e/fixtures/backup/azurite/azurite-deployment.yaml"
	azuriteServiceFile := "../e2e/fixtures/backup/azurite/azurite-service.yaml"
	// Create an Azurite for blob storage
	_, _, err := Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, azuriteDeploymentFile))
	if err != nil {
		return err
	}

	// Wait for the Azurite pod to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "azurite",
	}
	gomega.Eventually(func() (int32, error) {
		deployment := &apiv1.Deployment{}
		err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		return deployment.Status.ReadyReplicas, err
	}, 300).Should(gomega.BeEquivalentTo(1))

	// Create an Azurite service
	_, _, err = Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, azuriteServiceFile))
	return err
}

// InstallAzCli will install Az cli
func InstallAzCli(namespace string, env *TestingEnvironment) error {
	azCLiFile := "../e2e/fixtures/backup/azurite/az-cli.yaml"
	_, _, err := Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, azCLiFile))
	if err != nil {
		return err
	}
	azCliNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "az-cli",
	}
	gomega.Eventually(func() (bool, error) {
		az := &corev1.Pod{}
		err = env.Client.Get(env.Ctx, azCliNamespacedName, az)
		return utils.IsPodReady(*az), err
	}, 180).Should(gomega.BeTrue())
	return nil
}
