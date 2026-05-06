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
	"crypto/tls"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reconcileOperatorCertificateFingerprint", func() {
	var (
		r       *ClusterReconciler
		cluster *apiv1.Cluster
	)

	newReconcilerWithCert := func() (*ClusterReconciler, *tls.Certificate) {
		pair, err := certs.GenerateSelfSignedClientCertificate("test-operator")
		Expect(err).ToNot(HaveOccurred())
		tlsCert, err := pair.TLSCertificate()
		Expect(err).ToNot(HaveOccurred())

		scheme := schemeBuilder.BuildWithAllKnownScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&apiv1.Cluster{}).Build()

		return &ClusterReconciler{
			Client:             fakeClient,
			OperatorClientCert: &tlsCert,
		}, &tlsCert
	}

	BeforeEach(func() {
		var tlsCert *tls.Certificate
		r, tlsCert = newReconcilerWithCert()
		_ = tlsCert

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
		Expect(r.Client.Create(context.Background(), cluster)).To(Succeed())
	})

	It("patches the status and requeues when the fingerprint is missing", func() {
		result, err := r.reconcileOperatorCertificateFingerprint(context.Background(), cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		Expect(cluster.Status.OperatorCertificateFingerprint).ToNot(BeEmpty())
	})

	It("does not requeue when the fingerprint already matches", func() {
		// First call sets the fingerprint
		_, err := r.reconcileOperatorCertificateFingerprint(context.Background(), cluster)
		Expect(err).ToNot(HaveOccurred())

		// Second call finds it already correct
		result, err := r.reconcileOperatorCertificateFingerprint(context.Background(), cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("patches the status and requeues when the fingerprint is stale", func() {
		// Set a stale fingerprint
		cluster.Status.OperatorCertificateFingerprint = "stale-fingerprint"

		result, err := r.reconcileOperatorCertificateFingerprint(context.Background(), cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		Expect(cluster.Status.OperatorCertificateFingerprint).ToNot(Equal("stale-fingerprint"))
	})

	It("returns an error when no client certificate is set", func() {
		r.OperatorClientCert = nil
		_, err := r.reconcileOperatorCertificateFingerprint(context.Background(), cluster)
		Expect(err).To(HaveOccurred())
	})
})
