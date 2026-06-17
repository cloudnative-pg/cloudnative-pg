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
	"crypto/x509"
	"encoding/pem"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("databaserole_pki", func() {
	var (
		r         DatabaseRoleReconciler
		namespace string
		cluster   *apiv1.Cluster
	)

	BeforeEach(func() {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}, &apiv1.Cluster{}).
			Build()
		r = DatabaseRoleReconciler{Client: cli, Scheme: scheme}

		namespace = "default"
		cluster = newFakeCNPGCluster(cli, namespace)
	})

	newRole := func(name string, issueClientCert bool) *apiv1.DatabaseRole {
		role := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					Name:  name,
					Login: true,
				},
				ClusterRef: corev1.LocalObjectReference{Name: cluster.Name},
			},
		}
		if issueClientCert {
			// An unset enabled field defaults to true (IsClientCertificateEnabled).
			role.Spec.ClientCertificate = &apiv1.ClientCertificateConfiguration{}
		}
		// TypeMeta is needed for SetControllerReference to resolve the GVK.
		role.TypeMeta = metav1.TypeMeta{
			Kind:       "DatabaseRole",
			APIVersion: apiv1.SchemeGroupVersion.String(),
		}
		Expect(r.Create(GinkgoT().Context(), role)).To(Succeed())
		return role
	}

	certSecretKey := func(role *apiv1.DatabaseRole) types.NamespacedName {
		return types.NamespacedName{Name: role.GetClientCertSecretName(), Namespace: namespace}
	}

	Describe("issueClientCertificate", func() {
		It("creates the cert Secret and sets status.clientCertificate.expiration when CA is present", func(ctx SpecContext) {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			role := newRole("alice", true)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var certSecret corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &certSecret)).To(Succeed())

			// CN must equal the role name.
			certPEM := certSecret.Data[certs.TLSCertKey]
			block, _ := pem.Decode(certPEM)
			Expect(block).NotTo(BeNil())
			cert, err := x509.ParseCertificate(block.Bytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(cert.Subject.CommonName).To(Equal("alice"))

			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Expiration).NotTo(BeEmpty())
			Expect(role.Status.ClientCertificate.Message).To(BeEmpty())
		})

		It("keeps the cert Secret and reports expiration when cert is still valid (existing-secret path)",
			func(ctx SpecContext) {
				_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
				role := newRole("bob", true)

				// First reconcile: creates the secret.
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				firstExpiration := role.Status.ClientCertificate.Expiration

				// Second reconcile: secret already exists, renewal check runs.
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				var certSecret corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &certSecret)).To(Succeed())
				Expect(role.Status.ClientCertificate).NotTo(BeNil())
				Expect(role.Status.ClientCertificate.Expiration).To(Equal(firstExpiration))
			},
		)

		It("re-issues the cert when the cluster's client CA is rotated", func(ctx SpecContext) {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			role := newRole("ada", true)

			// First reconcile: creates the cert signed by the original CA.
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var firstSecret corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &firstSecret)).To(Succeed())
			firstCert := firstSecret.Data[certs.TLSCertKey]

			// Rotate the cluster's client CA: overwrite it with a brand new CA keypair.
			newCAPair, err := certs.CreateRootCA("test.example.com", namespace)
			Expect(err).NotTo(HaveOccurred())
			var caSecret corev1.Secret
			Expect(r.Get(ctx, types.NamespacedName{
				Name: cluster.GetClientCASecretName(), Namespace: namespace,
			}, &caSecret)).To(Succeed())
			caSecret.Data[certs.CACertKey] = newCAPair.Certificate
			caSecret.Data[certs.CAPrivateKeyKey] = newCAPair.Private
			Expect(r.Update(ctx, &caSecret)).To(Succeed())

			// Second reconcile: detects the CA change and re-issues.
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var secondSecret corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &secondSecret)).To(Succeed())
			secondCert := secondSecret.Data[certs.TLSCertKey]

			// The certificate must have been re-signed (bytes differ) and must
			// now validate against the rotated CA, preserving the CN.
			Expect(secondCert).NotTo(Equal(firstCert))
			signed, err := clientCertSignedByCurrentCA(&caSecret, &secondSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(signed).To(BeTrue())

			block, _ := pem.Decode(secondCert)
			Expect(block).NotTo(BeNil())
			cert, err := x509.ParseCertificate(block.Bytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(cert.Subject.CommonName).To(Equal("ada"))
		})

		It("sets status.clientCertificate.message and returns nil when CA has no private key", func(ctx SpecContext) {
			// Create a CA secret with only the certificate, no private key.
			_, caPair := generateFakeCASecret(r.Client, "tmp-ca", namespace, "test.example.com")
			caSecretWithoutKey := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.GetClientCASecretName(),
					Namespace: namespace,
				},
				Data: map[string][]byte{
					certs.CACertKey: caPair.Certificate,
					// deliberately omit CAPrivateKeyKey
				},
			}
			Expect(r.Create(ctx, caSecretWithoutKey)).To(Succeed())

			role := newRole("carol", true)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			// No cert secret should have been created.
			var certSecret corev1.Secret
			err := r.Get(ctx, certSecretKey(role), &certSecret)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(MatchError(ContainSubstring("not found")))

			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Message).To(ContainSubstring("no private key"))
			Expect(role.Status.ClientCertificate.Expiration).To(BeEmpty())
		})

		It("does nothing when CA secret is absent", func(ctx SpecContext) {
			role := newRole("dave", true)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var certSecret corev1.Secret
			err := r.Get(ctx, certSecretKey(role), &certSecret)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(MatchError(ContainSubstring("not found")))

			// Status untouched (nil) since we returned early.
			Expect(role.Status.ClientCertificate).To(BeNil())
		})

		It("leaves a same-named Secret it does not own untouched and reports a message", func(ctx SpecContext) {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			role := newRole("ivan", true)

			// Pre-create a Secret with the target name that is NOT owned by the role.
			unowned := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      role.GetClientCertSecretName(),
					Namespace: namespace,
				},
				Data: map[string][]byte{"sentinel": []byte("keep-me")},
			}
			Expect(r.Create(ctx, unowned)).To(Succeed())

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			// The foreign Secret must be left exactly as it was: not overwritten
			// with an operator-generated key pair.
			var got corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &got)).To(Succeed())
			Expect(got.Data).To(HaveKeyWithValue("sentinel", []byte("keep-me")))
			Expect(got.Data).NotTo(HaveKey(certs.TLSCertKey))

			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Message).To(ContainSubstring("not owned"))
			Expect(role.Status.ClientCertificate.Expiration).To(BeEmpty())
		})
	})

	Describe("deleteOwnedCertSecret", func() {
		It("deletes the owned cert Secret and clears status when clientCertificate is disabled",
			func(ctx SpecContext) {
				_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
				role := newRole("eve", true)

				// First issue the cert.
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				Expect(r.Get(ctx, certSecretKey(role), &corev1.Secret{})).To(Succeed())

				// Now opt out.
				role.Spec.ClientCertificate = nil
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				err := r.Get(ctx, certSecretKey(role), &corev1.Secret{})
				Expect(client.IgnoreNotFound(err)).To(Succeed())
				Expect(err).To(MatchError(ContainSubstring("not found")))

				Expect(role.Status.ClientCertificate).To(BeNil())
			},
		)

		It("clears status and does not error when cert Secret is already absent", func(ctx SpecContext) {
			role := newRole("frank", false)
			role.Status.ClientCertificate = &apiv1.ClientCertificateState{Expiration: "2099-01-01T00:00:00Z"}

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			Expect(role.Status.ClientCertificate).To(BeNil())
		})

		It("leaves an unowned Secret with the same name untouched", func(ctx SpecContext) {
			role := newRole("grace", false)

			// Create a Secret with the cert name but not owned by the role.
			unowned := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      role.GetClientCertSecretName(),
					Namespace: namespace,
				},
			}
			Expect(r.Create(ctx, unowned)).To(Succeed())

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			// The unowned secret must still exist.
			Expect(r.Get(ctx, certSecretKey(role), &corev1.Secret{})).To(Succeed())
			// Status must surface the conflict rather than silently dropping it.
			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Message).To(ContainSubstring("will not be deleted automatically"))
		})
	})

	Describe("clientCertificate not set (default)", func() {
		It("creates no Secret and sets no status", func(ctx SpecContext) {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			role := newRole("heidi", false)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			err := r.Get(ctx, certSecretKey(role), &corev1.Secret{})
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(MatchError(ContainSubstring("not found")))

			Expect(role.Status.ClientCertificate).To(BeNil())
		})
	})
})
