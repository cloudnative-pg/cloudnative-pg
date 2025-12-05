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

package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/repository"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakePluginRepository is a mock implementation of repository.Interface for testing
type fakePluginRepository struct {
	repository.Interface
	registeredPlugins map[string]*pluginRegistration
}

type pluginRegistration struct {
	address   string
	tlsConfig *tls.Config
}

func newFakePluginRepository() *fakePluginRepository {
	return &fakePluginRepository{
		registeredPlugins: make(map[string]*pluginRegistration),
	}
}

func (f *fakePluginRepository) RegisterRemotePlugin(
	name string,
	address string,
	tlsConfig *tls.Config,
) error {
	f.registeredPlugins[name] = &pluginRegistration{
		address:   address,
		tlsConfig: tlsConfig,
	}
	return nil
}

func (f *fakePluginRepository) ForgetPlugin(_ string) {}

// generateTestCertificate creates a self-signed certificate for testing with custom DNS names
func generateTestCertificate(dnsNames []string) (certPEM, keyPEM []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return certPEM, keyPEM, nil
}

var _ = Describe("PluginReconciler", func() {
	const (
		testNamespace    = "test-namespace"
		pluginName       = "test-plugin"
		serviceName      = "test-plugin-service"
		serverSecretName = "plugin-server-secret"
		clientSecretName = "plugin-client-secret"
		pluginPort       = "9090"
	)

	var (
		ctx              context.Context
		reconciler       *PluginReconciler
		fakeClient       client.Client
		pluginRepository *fakePluginRepository
		serverCertPEM    []byte
		serverKeyPEM     []byte
		clientCertPEM    []byte
		clientKeyPEM     []byte
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()

		// Generate test certificates
		serverCertPEM, serverKeyPEM, err = generateTestCertificate([]string{serviceName})
		Expect(err).ToNot(HaveOccurred())

		clientCertPEM, clientKeyPEM, err = generateTestCertificate([]string{"client"})
		Expect(err).ToNot(HaveOccurred())

		pluginRepository = newFakePluginRepository()

		// Create fake client with test objects
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithStatusSubresource(&corev1.Service{}).
			Build()

		reconciler = &PluginReconciler{
			Client:            fakeClient,
			Scheme:            scheme.BuildWithAllKnownScheme(),
			Plugins:           pluginRepository,
			OperatorNamespace: testNamespace,
		}
	})

	createPluginService := func(annotations map[string]string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: testNamespace,
				Labels: map[string]string{
					utils.PluginNameLabelName: pluginName,
				},
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 9090,
					},
				},
			},
		}
	}

	createSecret := func(name string, certPEM, keyPEM []byte) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       certPEM,
				corev1.TLSPrivateKeyKey: keyPEM,
			},
		}
	}

	Context("when reconciling a plugin service", func() {
		It("should use the service name as ServerName by default", func() {
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginClientSecretAnnotationName: clientSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
			}

			service := createPluginService(annotations)
			serverSecret := createSecret(serverSecretName, serverCertPEM, serverKeyPEM)
			clientSecret := createSecret(clientSecretName, clientCertPEM, clientKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, serverSecret)).To(Succeed())
			Expect(fakeClient.Create(ctx, clientSecret)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).ToNot(HaveOccurred())

			// Verify plugin was registered
			Expect(pluginRepository.registeredPlugins).To(HaveKey(pluginName))
			registration := pluginRepository.registeredPlugins[pluginName]
			Expect(registration.tlsConfig.ServerName).To(Equal(serviceName))
			Expect(registration.address).To(Equal(serviceName + ":" + pluginPort))
		})

		It("should use custom ServerName when annotation is provided", func() {
			customServerName := "barman-cloud.svc"
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginClientSecretAnnotationName: clientSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
				utils.PluginServerNameAnnotationName:   customServerName,
			}

			// Generate server certificate with custom DNS name
			customServerCertPEM, customServerKeyPEM, err := generateTestCertificate([]string{customServerName})
			Expect(err).ToNot(HaveOccurred())

			service := createPluginService(annotations)
			serverSecret := createSecret(serverSecretName, customServerCertPEM, customServerKeyPEM)
			clientSecret := createSecret(clientSecretName, clientCertPEM, clientKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, serverSecret)).To(Succeed())
			Expect(fakeClient.Create(ctx, clientSecret)).To(Succeed())

			_, err = reconciler.reconcile(ctx, service, pluginName)
			Expect(err).ToNot(HaveOccurred())

			// Verify plugin was registered with custom server name
			Expect(pluginRepository.registeredPlugins).To(HaveKey(pluginName))
			registration := pluginRepository.registeredPlugins[pluginName]
			Expect(registration.tlsConfig.ServerName).To(Equal(customServerName))
			Expect(registration.address).To(Equal(serviceName + ":" + pluginPort))
		})

		It("should skip reconciliation when server secret annotation is missing", func() {
			annotations := map[string]string{
				utils.PluginClientSecretAnnotationName: clientSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
			}

			service := createPluginService(annotations)
			Expect(fakeClient.Create(ctx, service)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).ToNot(HaveOccurred())

			// Verify plugin was not registered
			Expect(pluginRepository.registeredPlugins).ToNot(HaveKey(pluginName))
		})

		It("should skip reconciliation when client secret annotation is missing", func() {
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
			}

			service := createPluginService(annotations)
			serverSecret := createSecret(serverSecretName, serverCertPEM, serverKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, serverSecret)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).ToNot(HaveOccurred())

			// Verify plugin was not registered
			Expect(pluginRepository.registeredPlugins).ToNot(HaveKey(pluginName))
		})

		It("should skip reconciliation when port annotation is missing", func() {
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginClientSecretAnnotationName: clientSecretName,
			}

			service := createPluginService(annotations)
			serverSecret := createSecret(serverSecretName, serverCertPEM, serverKeyPEM)
			clientSecret := createSecret(clientSecretName, clientCertPEM, clientKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, serverSecret)).To(Succeed())
			Expect(fakeClient.Create(ctx, clientSecret)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).ToNot(HaveOccurred())

			// Verify plugin was not registered
			Expect(pluginRepository.registeredPlugins).ToNot(HaveKey(pluginName))
		})

		It("should return error when server secret does not exist", func() {
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginClientSecretAnnotationName: clientSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
			}

			service := createPluginService(annotations)
			clientSecret := createSecret(clientSecretName, clientCertPEM, clientKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, clientSecret)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).To(HaveOccurred())

			// Verify plugin was not registered
			Expect(pluginRepository.registeredPlugins).ToNot(HaveKey(pluginName))
		})

		It("should return error when client secret does not exist", func() {
			annotations := map[string]string{
				utils.PluginServerSecretAnnotationName: serverSecretName,
				utils.PluginClientSecretAnnotationName: clientSecretName,
				utils.PluginPortAnnotationName:         pluginPort,
			}

			service := createPluginService(annotations)
			serverSecret := createSecret(serverSecretName, serverCertPEM, serverKeyPEM)

			Expect(fakeClient.Create(ctx, service)).To(Succeed())
			Expect(fakeClient.Create(ctx, serverSecret)).To(Succeed())

			_, err := reconciler.reconcile(ctx, service, pluginName)
			Expect(err).To(HaveOccurred())

			// Verify plugin was not registered
			Expect(pluginRepository.registeredPlugins).ToNot(HaveKey(pluginName))
		})
	})
})
