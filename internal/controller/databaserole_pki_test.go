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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
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

	leafOf := func(secret corev1.Secret) *x509.Certificate {
		block, _ := pem.Decode(secret.Data[certs.TLSCertKey])
		Expect(block).NotTo(BeNil())
		cert, err := x509.ParseCertificate(block.Bytes)
		Expect(err).NotTo(HaveOccurred())
		return cert
	}

	Describe("issueClientCertificate", func() {
		It("creates the cert Secret and sets status.clientCertificate.expiration when CA is present", func(ctx SpecContext) {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			role := newRole("alice", true)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var certSecret corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &certSecret)).To(Succeed())

			// CN must equal the role name.
			Expect(leafOf(certSecret).Subject.CommonName).To(Equal("alice"))

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

				var certSecret corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &certSecret)).To(Succeed())
				firstCertBytes := certSecret.Data[certs.TLSCertKey]

				// Second reconcile: secret already exists, no renewal needed.
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				Expect(r.Get(ctx, certSecretKey(role), &certSecret)).To(Succeed())
				Expect(role.Status.ClientCertificate).NotTo(BeNil())
				Expect(role.Status.ClientCertificate.Expiration).To(Equal(firstExpiration))
				Expect(certSecret.Data[certs.TLSCertKey]).To(Equal(firstCertBytes))
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
			Expect(clientCertSignedByCurrentCA(
				ctx, &caSecret, &secondSecret, leafOf(secondSecret))).To(BeTrue())

			Expect(leafOf(secondSecret).Subject.CommonName).To(Equal("ada"))
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

		It("issues nothing and explains why when the CA secret is absent", func(ctx SpecContext) {
			role := newRole("dave", true)

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var certSecret corev1.Secret
			err := r.Get(ctx, certSecretKey(role), &certSecret)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(MatchError(ContainSubstring("not found")))

			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Message).To(
				ContainSubstring(cluster.GetClientCASecretName()))
			Expect(role.Status.ClientCertificate.Message).To(ContainSubstring("not found"))
			Expect(role.Status.ClientCertificate.Expiration).To(BeEmpty())
		})

		It("issues nothing and explains why when the cluster does not exist", func(ctx SpecContext) {
			role := newRole("frank", true)
			role.Spec.ClusterRef.Name = "typo-cluster"

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var certSecret corev1.Secret
			err := r.Get(ctx, certSecretKey(role), &certSecret)
			Expect(err).To(MatchError(ContainSubstring("not found")))

			Expect(role.Status.ClientCertificate).NotTo(BeNil())
			Expect(role.Status.ClientCertificate.Message).To(ContainSubstring(`Cluster "typo-cluster" not found`))
			Expect(role.Status.ClientCertificate.Expiration).To(BeEmpty())
		})

		It("keeps the expiration of a still-usable certificate when the cluster disappears",
			func(ctx SpecContext) {
				_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
				role := newRole("grace", true)

				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				expiration := role.Status.ClientCertificate.Expiration
				Expect(expiration).NotTo(BeEmpty())

				Expect(r.Delete(ctx, cluster)).To(Succeed())
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				// The certificate is still in the Secret and still valid, so the
				// administrator must still be able to tell when it runs out.
				Expect(role.Status.ClientCertificate.Expiration).To(Equal(expiration))
				Expect(role.Status.ClientCertificate.Message).To(ContainSubstring("not found"))
			},
		)

		It("drops the message once the reason it was recorded is gone", func(ctx SpecContext) {
			role := newRole("henry", true)

			// The CA secret is missing, so the first pass records why nothing
			// was issued.
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			Expect(role.Status.ClientCertificate.Message).NotTo(BeEmpty())

			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			// An administrator who fixes the cause reads the status to confirm
			// it, so a message that outlived its reason is worse than none.
			Expect(role.Status.ClientCertificate.Message).To(BeEmpty())
			Expect(role.Status.ClientCertificate.Expiration).NotTo(BeEmpty())
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

	Describe("client certificate duration and renewal", func() {
		BeforeEach(func() {
			_, _ = generateFakeCASecret(r.Client, cluster.GetClientCASecretName(), namespace, "test.example.com")
		})

		It("issues a certificate spanning the operator's default duration when neither field is set",
			func(ctx SpecContext) {
				role := newRole("wendy", true)
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				var secret corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &secret)).To(Succeed())
				cert := leafOf(secret)
				Expect(cert.NotAfter.Sub(cert.NotBefore)).To(Equal(90 * 24 * time.Hour))
			},
		)

		It("honors an explicit duration", func(ctx SpecContext) {
			role := newRole("xena", true)
			role.Spec.ClientCertificate.Duration = &metav1.Duration{Duration: 10 * 24 * time.Hour}

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var secret corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &secret)).To(Succeed())
			cert := leafOf(secret)
			Expect(cert.NotAfter.Sub(cert.NotBefore)).To(Equal(10 * 24 * time.Hour))
		})

		// A certificate is backdated to tolerate clock skew, so its usable life is
		// shorter than the requested duration. If a renewal window could cover
		// that difference, every freshly issued certificate would already be due
		// for renewal and the reconciler would re-issue it on every pass, with the
		// Secret write itself triggering the next pass. The API caps renewBefore at
		// half the lifetime to close that off; this asserts the resulting property
		// through the real signing path, across the range the API accepts.
		DescribeTable("never issues a certificate that is already inside its renewal window",
			func(ctx SpecContext, name string, duration, renewBefore *metav1.Duration) {
				role := newRole(name, true)
				role.Spec.ClientCertificate.Duration = duration
				role.Spec.ClientCertificate.RenewBefore = renewBefore

				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				var first corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
				leaf := leafOf(first)

				renewalDue := time.Now().Add(clientCertRenewBefore(role, clientCertDuration(role)))
				Expect(renewalDue).To(BeTemporally("<", leaf.NotAfter))

				// Therefore a second pass finds nothing to do.
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				var second corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
				Expect(second.Data[certs.TLSCertKey]).To(Equal(first.Data[certs.TLSCertKey]))
			},
			Entry("at the shortest lifetime the API allows, renewed as late as it allows",
				"yara", &metav1.Duration{Duration: time.Minute}, &metav1.Duration{Duration: 30 * time.Second}),
			Entry("just below the point where the skew allowance stops scaling",
				"yuri", &metav1.Duration{Duration: 49 * time.Minute}, &metav1.Duration{Duration: 24 * time.Minute}),
			Entry("just above it, where the allowance is a flat five minutes",
				"yusuf", &metav1.Duration{Duration: 51 * time.Minute}, &metav1.Duration{Duration: 25 * time.Minute}),
			Entry("with the renewal window left to the operator default",
				"yvonne", &metav1.Duration{Duration: time.Hour}, nil),
			Entry("with both left to the operator defaults", "yves", nil, nil),
		)

		It("re-issues the certificate when its private key is gone", func(ctx SpecContext) {
			role := newRole("yolanda", true)
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var first corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
			firstCert := first.Data[certs.TLSCertKey]

			delete(first.Data, certs.TLSPrivateKeyKey)
			Expect(r.Update(ctx, &first)).To(Succeed())

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var second corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
			Expect(second.Data[certs.TLSCertKey]).NotTo(Equal(firstCert))
			Expect(second.Data).To(HaveKey(certs.TLSPrivateKeyKey))
		})

		It("re-issues the certificate when its private key belongs to another certificate",
			func(ctx SpecContext) {
				role := newRole("yannick", true)
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				var first corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
				firstCert := first.Data[certs.TLSCertKey]

				// Swap in a valid private key that does not match the certificate.
				foreignRole := newRole("yannick-other", true)
				Expect(r.reconcileClientCertificate(ctx, foreignRole)).To(Succeed())
				var foreign corev1.Secret
				Expect(r.Get(ctx, certSecretKey(foreignRole), &foreign)).To(Succeed())
				first.Data[certs.TLSPrivateKeyKey] = foreign.Data[certs.TLSPrivateKeyKey]
				Expect(r.Update(ctx, &first)).To(Succeed())

				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				var second corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
				Expect(second.Data[certs.TLSCertKey]).NotTo(Equal(firstCert))
				_, err := certs.ParseServerSecret(&second)
				Expect(err).NotTo(HaveOccurred())
			},
		)

		It("does not write the Secret when the certificate needs nothing", func(ctx SpecContext) {
			role := newRole("yaron", true)
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var first corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var second corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
			Expect(second.ResourceVersion).To(Equal(first.ResourceVersion))
		})

		It("re-issues the certificate when duration shrinks on an existing role", func(ctx SpecContext) {
			role := newRole("aaron", true)
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var first corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
			firstCert := first.Data[certs.TLSCertKey]

			role.Spec.ClientCertificate.Duration = &metav1.Duration{Duration: 24 * time.Hour}
			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

			var second corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
			Expect(second.Data[certs.TLSCertKey]).NotTo(Equal(firstCert))
			cert := leafOf(second)
			Expect(cert.NotAfter.Sub(cert.NotBefore)).To(Equal(24 * time.Hour))
		})

		It("re-issues the certificate of a role that sets no duration when CERTIFICATE_DURATION changes",
			func(ctx SpecContext) {
				role := newRole("bruce", true)
				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
				var first corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
				Expect(leafOf(first).NotAfter.Sub(leafOf(first).NotBefore)).To(Equal(90 * 24 * time.Hour))

				configuration.Current.CertificateDuration = 30
				DeferCleanup(func() {
					configuration.Current = configuration.NewConfiguration()
				})

				Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())

				var second corev1.Secret
				Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
				Expect(second.Data[certs.TLSCertKey]).NotTo(Equal(first.Data[certs.TLSCertKey]))
				cert := leafOf(second)
				Expect(cert.NotAfter.Sub(cert.NotBefore)).To(Equal(30 * 24 * time.Hour))
			},
		)

		It("does not re-issue on consecutive reconciles when duration has a sub-second remainder "+
			"(the truncation guard)", func(ctx SpecContext) {
			role := newRole("carl", true)
			role.Spec.ClientCertificate.Duration = &metav1.Duration{Duration: 2*time.Hour + 500*time.Millisecond}

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var first corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &first)).To(Succeed())
			firstCert := first.Data[certs.TLSCertKey]

			Expect(r.reconcileClientCertificate(ctx, role)).To(Succeed())
			var second corev1.Secret
			Expect(r.Get(ctx, certSecretKey(role), &second)).To(Succeed())
			Expect(second.Data[certs.TLSCertKey]).To(Equal(firstCert))
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

var _ = Describe("clientCertNeedsRenewal", func() {
	It("renews a certificate that entered its renewal window", func() {
		leaf := &x509.Certificate{
			NotBefore: time.Now().Add(-50 * time.Second),
			NotAfter:  time.Now().Add(10 * time.Second),
		}
		Expect(clientCertNeedsRenewal(leaf, 30*time.Second)).To(BeTrue())
	})

	It("leaves a certificate short of its renewal window alone", func() {
		// A freshly issued one minute certificate, backdated six seconds for
		// clock skew: its renewal window opens twenty four seconds from now.
		leaf := &x509.Certificate{
			NotBefore: time.Now().Add(-6 * time.Second),
			NotAfter:  time.Now().Add(54 * time.Second),
		}
		Expect(clientCertNeedsRenewal(leaf, 30*time.Second)).To(BeFalse())
	})

	It("renews a certificate whose validity has not started yet", func() {
		// Signed by a clock running ahead of the current one: the expiry is far
		// away, but the certificate is refused until wall-clock time reaches
		// its notBefore, so waiting for the renewal window would leave the role
		// without a usable certificate.
		leaf := &x509.Certificate{
			NotBefore: time.Now().Add(time.Hour),
			NotAfter:  time.Now().Add(3 * time.Hour),
		}
		Expect(clientCertNeedsRenewal(leaf, 30*time.Second)).To(BeTrue())
	})
})

var _ = Describe("clientCertRequeueAfter", func() {
	roleWith := func(duration, renewBefore *metav1.Duration) *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{
				ClientCertificate: &apiv1.ClientCertificateConfiguration{
					Duration:    duration,
					RenewBefore: renewBefore,
				},
			},
		}
	}

	It("checks hourly at most, whatever the default lifetime allows", func() {
		Expect(clientCertRequeueAfter(roleWith(nil, nil))).To(Equal(clientCertReconcileInterval))
	})

	It("checks twice per renewal window for a short-lived certificate", func() {
		// The shortest configuration the API accepts: a one minute lifetime
		// renewed thirty seconds early, hence checked every fifteen seconds.
		requeue := clientCertRequeueAfter(roleWith(
			&metav1.Duration{Duration: time.Minute},
			&metav1.Duration{Duration: 30 * time.Second},
		))
		Expect(requeue).To(Equal(15 * time.Second))
	})

	It("halves the capped renewBefore when the role leaves it unset", func() {
		// renewBefore defaults to the operator threshold capped at half the
		// lifetime, so a two minute certificate renews one minute early and is
		// therefore checked every thirty seconds.
		requeue := clientCertRequeueAfter(roleWith(&metav1.Duration{Duration: 2 * time.Minute}, nil))
		Expect(requeue).To(Equal(30 * time.Second))
	})
})
