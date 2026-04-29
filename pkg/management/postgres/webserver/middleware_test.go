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

package webserver

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// parsedCert generates a self-signed cert and returns the parsed x509 leaf.
func parsedCert(cn string) *x509.Certificate {
	pair, err := certs.GenerateSelfSignedClientCertificate(cn)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	tlsCert, err := pair.TLSCertificate()
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return leaf
}

var _ = Describe("withOperatorAuth", func() {
	var (
		ws          remoteWebserverEndpoints
		nextCalled  bool
		nextHandler http.HandlerFunc
	)

	newInstance := func(fingerprint string) *postgres.Instance {
		instance := &postgres.Instance{}
		cluster := &apiv1.Cluster{}
		cluster.Status.OperatorCertificateFingerprint = fingerprint
		instance.SetCluster(cluster)
		return instance
	}

	BeforeEach(func() {
		nextCalled = false
		nextHandler = func(_ http.ResponseWriter, _ *http.Request) {
			nextCalled = true
		}
	})

	It("rejects a request with no TLS state", func() {
		ws = remoteWebserverEndpoints{instance: newInstance("some-fingerprint")}
		req := httptest.NewRequest(http.MethodGet, "/backup", nil)
		// req.TLS is nil by default
		w := httptest.NewRecorder()

		ws.withOperatorAuth(nextHandler)(w, req)

		Expect(w.Code).To(Equal(http.StatusUnauthorized))
		Expect(nextCalled).To(BeFalse())
	})

	It("rejects a request with TLS but no peer certificates", func() {
		ws = remoteWebserverEndpoints{instance: newInstance("some-fingerprint")}
		req := httptest.NewRequest(http.MethodGet, "/backup", nil)
		req.TLS = &tls.ConnectionState{} // no PeerCertificates
		w := httptest.NewRecorder()

		ws.withOperatorAuth(nextHandler)(w, req)

		Expect(w.Code).To(Equal(http.StatusUnauthorized))
		Expect(nextCalled).To(BeFalse())
	})

	It("returns 503 when no fingerprint is stored in the cluster status", func() {
		ws = remoteWebserverEndpoints{instance: newInstance("")}

		leaf := parsedCert("test")
		req := httptest.NewRequest(http.MethodGet, "/backup", nil)
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
		w := httptest.NewRecorder()

		ws.withOperatorAuth(nextHandler)(w, req)

		Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(nextCalled).To(BeFalse())
	})

	It("rejects a request whose certificate fingerprint does not match", func() {
		ws = remoteWebserverEndpoints{instance: newInstance("wrong-fingerprint")}

		leaf := parsedCert("test")
		req := httptest.NewRequest(http.MethodGet, "/backup", nil)
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
		w := httptest.NewRecorder()

		ws.withOperatorAuth(nextHandler)(w, req)

		Expect(w.Code).To(Equal(http.StatusUnauthorized))
		Expect(nextCalled).To(BeFalse())
	})

	It("calls the next handler when the fingerprint matches", func() {
		leaf := parsedCert("test")
		fingerprint := certs.PublicKeyFingerprint(leaf)
		ws = remoteWebserverEndpoints{instance: newInstance(fingerprint)}

		req := httptest.NewRequest(http.MethodGet, "/backup", nil)
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
		w := httptest.NewRecorder()

		ws.withOperatorAuth(nextHandler)(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(nextCalled).To(BeTrue())
	})
})
