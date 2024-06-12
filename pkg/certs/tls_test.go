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

package certs

import (
	"context"
	"crypto/tls"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newTLSConfigFromSecret", func() {
	var (
		c            client.Client
		caSecret     types.NamespacedName
		serverSecret types.NamespacedName
	)

	BeforeEach(func() {
		caSecret = types.NamespacedName{Name: "test-ca-secret", Namespace: "default"}
		serverSecret = types.NamespacedName{Name: "test-server-secret", Namespace: "default"}
	})

	Context("when the secret is found and valid", func() {
		BeforeEach(func() {
			caData := map[string][]byte{
				CACertKey: []byte(`-----BEGIN CERTIFICATE-----
MIICPjCCAcSgAwIBAgIUJsgJfS4wtL/nVC28UHtSeM3yYjwwCgYIKoZIzj0EAwIw
VjELMAkGA1UEBhMCSVQxDjAMBgNVBAgMBVByYXRvMQ4wDAYDVQQHDAVQcmF0bzEM
MAoGA1UECgwDRURCMQwwCgYDVQQLDANXRUIxCzAJBgNVBAMMAkNBMB4XDTI0MDYx
MjEyMzkxMFoXDTM0MDYxMDEyMzkxMFowVjELMAkGA1UEBhMCSVQxDjAMBgNVBAgM
BVByYXRvMQ4wDAYDVQQHDAVQcmF0bzEMMAoGA1UECgwDRURCMQwwCgYDVQQLDANX
RUIxCzAJBgNVBAMMAkNBMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAETWFM9TA5Qcgq
enACpSG2qYc/Or7OYbDY74ng0ZrQPcUUL3QbMzrx7wYhA4gECgLdBVqYivgqml37
bks7DhhYe4h0/0jlqL0cqTzWJxrEbnCoDFDJ1FfJJ02ID/TdtvsSo1MwUTAdBgNV
HQ4EFgQUd2X2XXcREi+k1PQV9NRFjKrwCKkwHwYDVR0jBBgwFoAUd2X2XXcREi+k
1PQV9NRFjKrwCKkwDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNoADBlAjEA
9CcEvnf75XYQBtGoYsecBkVFfCGxJPXjBYS/5sOCcx7jX0bo8pQn0UMvadN2I2kM
AjABtpzmdjSn59hozhmg14x6KrL2OBS2W+PchpaJ5+brX7krRH/PWsiGDZAI1+xx
Wis=
-----END CERTIFICATE-----`),
			}
			serverData := map[string][]byte{
				TLSCertKey: []byte(`-----BEGIN CERTIFICATE-----
MIIDSjCCAtGgAwIBAgIUKqMcWodLQqR5RQoSFIjJm2FhPM4wCgYIKoZIzj0EAwIw
VjELMAkGA1UEBhMCSVQxDjAMBgNVBAgMBVByYXRvMQ4wDAYDVQQHDAVQcmF0bzEM
MAoGA1UECgwDRURCMQwwCgYDVQQLDANXRUIxCzAJBgNVBAMMAkNBMB4XDTI0MDYx
MjEyMzkxMFoXDTM0MDYxMDEyMzkxMFowWjELMAkGA1UEBhMCSVQxDjAMBgNVBAgM
BVByYXRvMQ4wDAYDVQQHDAVQcmF0bzEMMAoGA1UECgwDRURCMQwwCgYDVQQLDANX
RUIxDzANBgNVBAMMBnNlcnZlcjB2MBAGByqGSM49AgEGBSuBBAAiA2IABOW9IUt6
c8AEcotrYEDNp2u8daGAamiQdlfWk9qOokPhIMi9bfEJaV6gppzmONmtvwSGgAyZ
rsAdNTSiug8jl4aX3P7r+OMGeFojSVqGfT+DohEk5yPSL99zmy2PTQDbd6OCAVow
ggFWMAkGA1UdEwQCMAAwEQYJYIZIAYb4QgEBBAQDAgZAMDMGCWCGSAGG+EIBDQQm
FiRPcGVuU1NMIEdlbmVyYXRlZCBTZXJ2ZXIgQ2VydGlmaWNhdGUwHQYDVR0OBBYE
FF5PHGe9xOrp6lb1ehg5aJcmwiVHMIGTBgNVHSMEgYswgYiAFHdl9l13ERIvpNT0
FfTURYyq8AipoVqkWDBWMQswCQYDVQQGEwJJVDEOMAwGA1UECAwFUHJhdG8xDjAM
BgNVBAcMBVByYXRvMQwwCgYDVQQKDANFREIxDDAKBgNVBAsMA1dFQjELMAkGA1UE
AwwCQ0GCFCbICX0uMLS/51QtvFB7UnjN8mI8MA4GA1UdDwEB/wQEAwIFoDAdBgNV
HSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwHQYDVR0RBBYwFIISc2VydmVyLnBy
aXZhdGUudGxkMAoGCCqGSM49BAMCA2cAMGQCMATaIxLFrs1Jl9NHWbor1YQ74tyV
ezD8cxjuSvVGLqJGY0KO5QQqhvi/pgOziUX4VwIwfjH/6u/0HCV813pb1BK5qJvD
vF0yoWNwrkGqVn9d/cKVnGCNWz3FyF+AheA5kOni
-----END CERTIFICATE-----
`),
				TLSPrivateKeyKey: []byte(`-----BEGIN EC PARAMETERS-----
BgUrgQQAIg==
-----END EC PARAMETERS-----
-----BEGIN EC PRIVATE KEY-----
MIGkAgEBBDADWXMBEMbLt7RKCOFpbYxwumKYA+Mkw/V19aibN/j8oS/uN7Qz+BIJ
+5Lkd3OUhPagBwYFK4EEACKhZANiAATlvSFLenPABHKLa2BAzadrvHWhgGpokHZX
1pPajqJD4SDIvW3xCWleoKac5jjZrb8EhoAMma7AHTU0oroPI5eGl9z+6/jjBnha
I0lahn0/g6IRJOcj0i/fc5stj00A23c=
-----END EC PRIVATE KEY-----
`),
			}
			ca := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
				Data: caData,
			}
			server := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverSecret.Name,
					Namespace: serverSecret.Namespace,
				},
				Data: serverData,
			}
			c = fake.NewClientBuilder().WithObjects(ca, server).Build()
		})

		It("should return a valid tls.Config", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret, serverSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(tlsConfig).NotTo(BeNil())
			Expect(tlsConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
			Expect(tlsConfig.ServerName).To(Equal(`server`))
			Expect(tlsConfig.RootCAs).ToNot(BeNil())
		})
	})

	Context("when the secret is not found", func() {
		BeforeEach(func() {
			c = fake.NewClientBuilder().Build()
		})

		It("should return an error", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret, serverSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("while getting CA secret %s", caSecret)))
		})
	})

	Context("when the ca.crt entry is missing in the secret", func() {
		BeforeEach(func() {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
			}
			c = fake.NewClientBuilder().WithObjects(secret).Build()
		})

		It("should return an error", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret, serverSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("missing %s entry in secret %s", CACertKey, caSecret)))
		})
	})
})
