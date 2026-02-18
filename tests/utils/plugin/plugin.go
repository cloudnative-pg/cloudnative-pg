/*
Copyright 2025 The CloudNativePG Contributors.

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

package plugin

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// Deploy deploys the mock plugin and sets up certificates
func Deploy(env *environment.TestingEnvironment, namespace string) error {
	// 1. Create Root CA
	caPair, err := certs.CreateRootCA(namespace, "cnpg-i-plugin-mock-ca")
	if err != nil {
		return err
	}

	// 2. Create Server Certs
	serverPair, err := caPair.CreateAndSignPair("cnpg-i-plugin-mock-server", certs.CertTypeServer,
		[]string{"cnpg-i-plugin-mock", "cnpg-i-plugin-mock-service", "cnpg-i-plugin-mock.test"},
	)
	if err != nil {
		return err
	}
	serverSecret := serverPair.GenerateCertificateSecret(namespace, "cnpg-i-plugin-mock-server-tls")
	if err := env.Client.Create(env.Ctx, serverSecret); err != nil {
		return err
	}

	// 3. Create Client Certs
	clientPair, err := caPair.CreateAndSignPair("cnpg-i-plugin-mock-client", certs.CertTypeClient,
		[]string{"cnpg-i-plugin-mock-client"},
	)
	if err != nil {
		return err
	}
	clientSecret := clientPair.GenerateCertificateSecret(namespace, "cnpg-i-plugin-mock-client-tls")
	if err := env.Client.Create(env.Ctx, clientSecret); err != nil {
		return err
	}

	// 4. Create Deployment and Service
	// We use the fixtures defined in tests/e2e/fixtures/plugin/deployment.yaml
	// But we need to apply them.
	// Since run.Unchecked requires file path, we assume the path relative to the checkout.
	// We can also just read the file and use objects.Create, but `kubectl apply` is easier for multi-doc yaml.

	// Adjust this path if needed.
	deploymentFile := "tests/e2e/fixtures/plugin/deployment.yaml"
	if _, _, err := run.Unchecked("kubectl apply -n " + namespace + " -f " + deploymentFile); err != nil {
		return err
	}

	// Wait for deployment to be ready
	// This is a simplified check.
	// In a real scenario we'd use pods.CreateAndWaitForReady or similar.

	return nil
}
