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
	"crypto/x509"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setupPostgresPKI", func() {
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pki-test",
				Namespace: "default",
			},
		}
	})

	It("succeeds in the default configuration even when the informer cache is stale "+
		"for the shared CA secret", func(ctx SpecContext) {
		// In the default configuration the server CA and the client CA resolve to the
		// same secret name. Simulate the informer cache lagging behind the API server:
		// every Get for that secret returns NotFound, even right after it was created.
		// With the fix, the already-fetched server CA is reused and no second
		// Get/Create is attempted; without it, the redundant Create fails with
		// AlreadyExists and setupPostgresPKI returns an error.
		Expect(cluster.GetServerCASecretName()).To(Equal(cluster.GetClientCASecretName()))
		caName := cluster.GetServerCASecretName()

		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption,
				) error {
					if _, ok := obj.(*corev1.Secret); ok && key.Name == caName {
						return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			}).
			Build()

		r := &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(10),
		}

		Expect(r.setupPostgresPKI(ctx, cluster)).To(Succeed())

		// The CA secret must have been created exactly once, and the full PKI provisioned.
		var secrets corev1.SecretList
		Expect(r.Client.List(ctx, &secrets, client.InNamespace(cluster.Namespace))).To(Succeed())
		names := map[string]int{}
		for i := range secrets.Items {
			names[secrets.Items[i].Name]++
		}
		Expect(names[caName]).To(Equal(1), "the shared CA secret must be created exactly once")
		Expect(names).To(HaveKey(cluster.GetServerTLSSecretName()))
		Expect(names).To(HaveKey(cluster.GetReplicationSecretName()))
	})

	It("provisions client certificates from a separate client CA when the names differ",
		func(ctx SpecContext) {
			// A user-supplied client CA with a different name must still be honored:
			// the replication client certificate has to be signed by it, not by the
			// (generated) server CA.
			cluster.Spec.Certificates = &apiv1.CertificatesConfiguration{
				ClientCASecret: "custom-client-ca",
			}
			Expect(cluster.GetServerCASecretName()).ToNot(Equal(cluster.GetClientCASecretName()))

			caPair, err := certs.CreateRootCA(cluster.Name, cluster.Namespace)
			Expect(err).ToNot(HaveOccurred())
			clientCASecret := caPair.GenerateCASecret(cluster.Namespace, "custom-client-ca")

			fakeClient := fake.NewClientBuilder().
				WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
				WithObjects(clientCASecret).
				Build()

			r := &ClusterReconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			Expect(r.setupPostgresPKI(ctx, cluster)).To(Succeed())

			get := func(name string) *corev1.Secret {
				var secret corev1.Secret
				Expect(r.Client.Get(ctx,
					client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret)).To(Succeed())
				return &secret
			}
			serverCA := get(cluster.GetServerCASecretName())
			clientCA := get(cluster.GetClientCASecretName())
			replication := get(cluster.GetReplicationSecretName())

			// validateLeafCertificate appends the CA to opts.Roots, so each call needs
			// its own VerifyOptions to avoid accumulating trusted roots across calls.
			clientAuthOpts := func() *x509.VerifyOptions {
				return &x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
			}
			Expect(validateLeafCertificate(clientCA, replication, clientAuthOpts())).To(Succeed(),
				"the replication certificate must be signed by the separate client CA")
			Expect(validateLeafCertificate(serverCA, replication, clientAuthOpts())).ToNot(Succeed(),
				"the replication certificate must not be signed by the server CA")
		})
})
