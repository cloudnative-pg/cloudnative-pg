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

package run

import (
	"crypto/tls"

	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("runOptions.metricsTLSConfig", func() {
	It("returns nil when metrics TLS is disabled", func() {
		opts := runOptions{
			poolerNamespacedName: types.NamespacedName{Name: "p", Namespace: "ns"},
			metricsPortTLS:       false,
		}
		Expect(opts.metricsTLSConfig()).To(BeNil())
	})

	It("returns a TLS 1.3 config with a reloading GetCertificate when enabled", func() {
		opts := runOptions{metricsPortTLS: true}

		cfg := opts.metricsTLSConfig()
		Expect(cfg).ToNot(BeNil())
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(cfg.GetCertificate).ToNot(BeNil())
	})

	It("propagates the read error when the cert files are missing", func() {
		opts := runOptions{metricsPortTLS: true}

		// The production paths (config.ClientTLSCertPath / ClientTLSKeyPath)
		// point at /controller/configs/client-tls/*, which does not exist in
		// the test environment. GetCertificate must surface the failure rather
		// than returning a nil error with a nil certificate.
		cert, err := opts.metricsTLSConfig().GetCertificate(&tls.ClientHelloInfo{})
		Expect(err).To(HaveOccurred())
		Expect(cert).To(BeNil())
	})
})
