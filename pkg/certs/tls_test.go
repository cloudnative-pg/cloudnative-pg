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

package certs

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newTLSConfigFromSecret", func() {
	var (
		c        client.Client
		caSecret types.NamespacedName
	)

	BeforeEach(func() {
		caSecret = types.NamespacedName{Name: "test-secret", Namespace: "default"}
	})

	Context("when the secret is found and valid", func() {
		BeforeEach(func() {
			caData := map[string][]byte{
				CACertKey: []byte(`
Certificate:
    Data:
        Version: 3 (0x2)
        Serial Number:
            66:10:89:ae:f9:55:99:81:9c:34:cc:ff:1e:86:e8:7e:3c:47:61:34
        Signature Algorithm: ecdsa-with-SHA256
        Issuer: CN=CA
        Validity
            Not Before: Jun 18 15:36:59 2024 GMT
            Not After : Jun 16 15:36:59 2034 GMT
        Subject: CN=CA
        Subject Public Key Info:
            Public Key Algorithm: id-ecPublicKey
                Public-Key: (384 bit)
                pub:
                    04:8f:69:ab:43:73:b9:1a:38:03:38:5f:e6:ec:9e:
                    7f:1e:9a:bd:96:82:7f:aa:3d:f9:1f:63:ae:5a:7a:
                    a6:c2:c4:38:0a:d2:9e:27:38:9f:ae:51:2d:98:db:
                    86:32:0f:d5:17:dd:77:73:56:67:08:71:51:5a:bb:
                    54:48:d7:26:fe:35:b0:d0:04:e5:4d:61:71:86:16:
                    41:4a:5b:9c:b2:fd:4d:39:9f:8f:60:2b:40:81:62:
                    a6:b6:4f:92:4d:ae:1e
                ASN1 OID: secp384r1
                NIST CURVE: P-384
        X509v3 extensions:
            X509v3 Subject Key Identifier:
                6F:18:E5:45:77:82:87:82:D5:C2:4D:21:18:7B:7D:51:07:F1:60:5F
            X509v3 Authority Key Identifier:
                6F:18:E5:45:77:82:87:82:D5:C2:4D:21:18:7B:7D:51:07:F1:60:5F
            X509v3 Basic Constraints: critical
                CA:TRUE
    Signature Algorithm: ecdsa-with-SHA256
    Signature Value:
        30:65:02:30:05:da:f0:d9:a9:f0:a1:b0:a7:00:51:7b:ab:eb:
        42:c6:5d:a8:5c:40:a5:4b:ca:0d:99:3d:98:6e:2c:cd:00:7e:
        e8:63:19:6d:24:ef:63:c0:30:5e:25:cb:be:a0:ca:40:02:31:
        00:df:04:a0:53:93:81:52:48:17:90:28:e2:6f:b7:47:3d:71:
        06:7c:11:0b:37:dc:ae:14:9f:12:86:9b:fb:26:b3:1e:a7:8f:
        76:75:20:09:b5:76:bf:27:db:ab:76:70:73
-----BEGIN CERTIFICATE-----
MIIBrDCCATKgAwIBAgIUZhCJrvlVmYGcNMz/HobofjxHYTQwCgYIKoZIzj0EAwIw
DTELMAkGA1UEAwwCQ0EwHhcNMjQwNjE4MTUzNjU5WhcNMzQwNjE2MTUzNjU5WjAN
MQswCQYDVQQDDAJDQTB2MBAGByqGSM49AgEGBSuBBAAiA2IABI9pq0NzuRo4Azhf
5uyefx6avZaCf6o9+R9jrlp6psLEOArSnic4n65RLZjbhjIP1Rfdd3NWZwhxUVq7
VEjXJv41sNAE5U1hcYYWQUpbnLL9TTmfj2ArQIFiprZPkk2uHqNTMFEwHQYDVR0O
BBYEFG8Y5UV3goeC1cJNIRh7fVEH8WBfMB8GA1UdIwQYMBaAFG8Y5UV3goeC1cJN
IRh7fVEH8WBfMA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDaAAwZQIwBdrw
2anwobCnAFF7q+tCxl2oXEClS8oNmT2YbizNAH7oYxltJO9jwDBeJcu+oMpAAjEA
3wSgU5OBUkgXkCjib7dHPXEGfBELN9yuFJ8Shpv7JrMep492dSAJtXa/J9urdnBz
-----END CERTIFICATE-----
`),
			}
			ca := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
				Data: caData,
			}
			c = fake.NewClientBuilder().WithObjects(ca).Build()
		})

		It("should return a valid tls.Config", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(tlsConfig).NotTo(BeNil())
			Expect(tlsConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
			Expect(tlsConfig.RootCAs).ToNot(BeNil())
		})

		It("should validate good certificates", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).NotTo(HaveOccurred())
			serverBlock, _ := pem.Decode([]byte(`
Certificate:
    Data:
        Version: 3 (0x2)
        Serial Number:
            79:eb:2b:67:38:42:f3:39:b1:3c:2e:25:28:fb:53:56:b5:9a:4b:e1
        Signature Algorithm: ecdsa-with-SHA256
        Issuer: CN=CA
        Validity
            Not Before: Jun 18 15:36:59 2024 GMT
            Not After : Jun 16 15:36:59 2034 GMT
        Subject: CN=server
        Subject Public Key Info:
            Public Key Algorithm: id-ecPublicKey
                Public-Key: (384 bit)
                pub:
                    04:79:7f:27:60:cc:25:b1:cf:d4:4a:06:a6:86:8e:
                    66:1f:e8:1f:dc:1b:1a:fb:3f:ea:74:ec:3f:ca:c1:
                    68:ac:b1:e1:e7:68:53:98:f1:f7:35:9a:b1:c5:c5:
                    b3:9a:9f:1b:8d:ab:2f:06:b4:79:2a:10:af:c5:c6:
                    e7:22:82:93:81:9c:f1:65:34:69:ba:b9:aa:09:48:
                    3a:da:dd:a4:52:5b:a1:58:6a:8a:d8:71:b1:eb:78:
                    9f:88:b3:32:dd:71:b0
                ASN1 OID: secp384r1
                NIST CURVE: P-384
        X509v3 extensions:
            X509v3 Basic Constraints:
                CA:FALSE
            Netscape Cert Type:
                SSL Server
            Netscape Comment:
                OpenSSL Generated Server Certificate
            X509v3 Subject Key Identifier:
                CA:71:9F:5C:D0:C4:1C:12:D4:60:5E:9C:05:A3:84:F4:FF:56:E1:1E
            X509v3 Authority Key Identifier:
                keyid:6F:18:E5:45:77:82:87:82:D5:C2:4D:21:18:7B:7D:51:07:F1:60:5F
                DirName:/CN=CA
                serial:66:10:89:AE:F9:55:99:81:9C:34:CC:FF:1E:86:E8:7E:3C:47:61:34
            X509v3 Key Usage: critical
                Digital Signature, Key Encipherment
            X509v3 Extended Key Usage:
                TLS Web Server Authentication, TLS Web Client Authentication
            X509v3 Subject Alternative Name:
                DNS:server.private.tld
    Signature Algorithm: ecdsa-with-SHA256
    Signature Value:
        30:64:02:30:3c:af:af:1f:0c:ed:44:d9:79:92:42:d4:a8:dc:
        9c:9b:b1:26:5e:fe:e8:0f:1f:8e:a1:dd:66:1f:f2:fc:81:72:
        89:93:42:f5:74:6a:a2:ea:96:4d:3d:c9:a8:8e:c1:40:02:30:
        67:18:f5:7f:15:52:99:4c:b5:4c:15:f3:e8:7d:2c:52:fb:45:
        87:f1:60:6f:ab:f8:a9:43:dd:44:4e:b1:34:9c:37:95:b6:54:
        67:11:eb:db:15:e4:e4:ea:7f:0b:0e:8e
-----BEGIN CERTIFICATE-----
MIICbDCCAfOgAwIBAgIUeesrZzhC8zmxPC4lKPtTVrWaS+EwCgYIKoZIzj0EAwIw
DTELMAkGA1UEAwwCQ0EwHhcNMjQwNjE4MTUzNjU5WhcNMzQwNjE2MTUzNjU5WjAR
MQ8wDQYDVQQDDAZzZXJ2ZXIwdjAQBgcqhkjOPQIBBgUrgQQAIgNiAAR5fydgzCWx
z9RKBqaGjmYf6B/cGxr7P+p07D/KwWisseHnaFOY8fc1mrHFxbOanxuNqy8GtHkq
EK/FxucigpOBnPFlNGm6uaoJSDra3aRSW6FYaorYcbHreJ+IszLdcbCjggEOMIIB
CjAJBgNVHRMEAjAAMBEGCWCGSAGG+EIBAQQEAwIGQDAzBglghkgBhvhCAQ0EJhYk
T3BlblNTTCBHZW5lcmF0ZWQgU2VydmVyIENlcnRpZmljYXRlMB0GA1UdDgQWBBTK
cZ9c0MQcEtRgXpwFo4T0/1bhHjBIBgNVHSMEQTA/gBRvGOVFd4KHgtXCTSEYe31R
B/FgX6ERpA8wDTELMAkGA1UEAwwCQ0GCFGYQia75VZmBnDTM/x6G6H48R2E0MA4G
A1UdDwEB/wQEAwIFoDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwHQYD
VR0RBBYwFIISc2VydmVyLnByaXZhdGUudGxkMAoGCCqGSM49BAMCA2cAMGQCMDyv
rx8M7UTZeZJC1KjcnJuxJl7+6A8fjqHdZh/y/IFyiZNC9XRqouqWTT3JqI7BQAIw
Zxj1fxVSmUy1TBXz6H0sUvtFh/Fgb6v4qUPdRE6xNJw3lbZUZxHr2xXk5Op/Cw6O
-----END CERTIFICATE-----
`))
			err = tlsConfig.VerifyPeerCertificate([][]byte{serverBlock.Bytes}, nil)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should reject bad certificates", func(ctx context.Context) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).NotTo(HaveOccurred())
			badServerBlock, _ := pem.Decode([]byte(`Certificate:
    Data:
        Version: 3 (0x2)
        Serial Number:
            79:eb:2b:67:38:42:f3:39:b1:3c:2e:25:28:fb:53:56:b5:9a:4b:e2
        Signature Algorithm: ecdsa-with-SHA256
        Issuer: CN=CA
        Validity
            Not Before: Jun 18 16:01:44 2024 GMT
            Not After : Jun 16 16:01:44 2034 GMT
        Subject: CN=server
        Subject Public Key Info:
            Public Key Algorithm: id-ecPublicKey
                Public-Key: (384 bit)
                pub:
                    04:9a:14:de:61:60:87:c8:de:53:54:29:56:04:db:
                    5a:0c:7c:45:cf:ef:4e:62:1c:dc:f3:98:45:4d:2e:
                    f8:34:6b:70:05:ab:06:ff:37:fb:e2:56:3c:b1:f3:
                    ee:7f:23:32:c0:5b:f2:9c:09:99:e7:d8:d7:7c:84:
                    c4:d8:4c:01:51:c1:24:9b:ac:d8:cb:b9:97:48:01:
                    32:1e:0b:16:6c:bb:1a:b1:9d:d3:e2:51:c4:a1:39:
                    65:61:a2:bf:81:bd:78
                ASN1 OID: secp384r1
                NIST CURVE: P-384
        X509v3 extensions:
            X509v3 Basic Constraints:
                CA:FALSE
            Netscape Cert Type:
                SSL Server
            Netscape Comment:
                OpenSSL Generated Server Certificate
            X509v3 Subject Key Identifier:
                5D:53:DE:D3:60:C9:77:C6:E9:48:FF:B9:AA:27:44:DF:DF:73:C7:61
            X509v3 Authority Key Identifier:
                keyid:0B:71:A6:BF:D0:1D:23:64:26:24:B2:E3:FA:32:48:A7:F6:81:C1:CA
                DirName:/CN=CA
                serial:41:EF:37:0F:BE:78:0B:72:63:75:C5:71:85:44:D8:EC:F3:D7:65:45
            X509v3 Key Usage: critical
                Digital Signature, Key Encipherment
            X509v3 Extended Key Usage:
                TLS Web Server Authentication, TLS Web Client Authentication
            X509v3 Subject Alternative Name:
                DNS:server.private.tld
    Signature Algorithm: ecdsa-with-SHA256
    Signature Value:
        30:66:02:31:00:f7:14:2c:d0:2a:8a:3a:a7:43:1e:f6:82:fe:
        40:24:e7:8d:e1:47:d8:71:8b:8c:5f:8a:03:fa:ac:c1:a2:a9:
        99:89:a5:06:e8:7a:9d:76:73:e0:5c:8c:db:0e:c6:43:f6:02:
        31:00:8a:1a:a2:1d:f9:78:fa:3b:a8:27:a2:2f:71:86:ed:2b:
        6f:34:a7:32:3a:d4:46:86:b5:bf:67:79:f8:ee:57:b2:c1:3b:
        2c:6b:49:74:82:ab:77:6a:7b:12:ec:04:e9:d9
-----BEGIN CERTIFICATE-----
MIICbjCCAfOgAwIBAgIUeesrZzhC8zmxPC4lKPtTVrWaS+IwCgYIKoZIzj0EAwIw
DTELMAkGA1UEAwwCQ0EwHhcNMjQwNjE4MTYwMTQ0WhcNMzQwNjE2MTYwMTQ0WjAR
MQ8wDQYDVQQDDAZzZXJ2ZXIwdjAQBgcqhkjOPQIBBgUrgQQAIgNiAASaFN5hYIfI
3lNUKVYE21oMfEXP705iHNzzmEVNLvg0a3AFqwb/N/viVjyx8+5/IzLAW/KcCZnn
2Nd8hMTYTAFRwSSbrNjLuZdIATIeCxZsuxqxndPiUcShOWVhor+BvXijggEOMIIB
CjAJBgNVHRMEAjAAMBEGCWCGSAGG+EIBAQQEAwIGQDAzBglghkgBhvhCAQ0EJhYk
T3BlblNTTCBHZW5lcmF0ZWQgU2VydmVyIENlcnRpZmljYXRlMB0GA1UdDgQWBBRd
U97TYMl3xulI/7mqJ0Tf33PHYTBIBgNVHSMEQTA/gBQLcaa/0B0jZCYksuP6Mkin
9oHByqERpA8wDTELMAkGA1UEAwwCQ0GCFEHvNw++eAtyY3XFcYVE2Ozz12VFMA4G
A1UdDwEB/wQEAwIFoDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwHQYD
VR0RBBYwFIISc2VydmVyLnByaXZhdGUudGxkMAoGCCqGSM49BAMCA2kAMGYCMQD3
FCzQKoo6p0Me9oL+QCTnjeFH2HGLjF+KA/qswaKpmYmlBuh6nXZz4FyM2w7GQ/YC
MQCKGqId+Xj6O6gnoi9xhu0rbzSnMjrURoa1v2d5+O5XssE7LGtJdIKrd2p7EuwE
6dk=
-----END CERTIFICATE-----
`))
			err = tlsConfig.VerifyPeerCertificate([][]byte{badServerBlock.Bytes}, nil)
			var certError *tls.CertificateVerificationError
			Expect(errors.As(err, &certError)).To(BeTrue())
		})
	})

	Context("when the secret is not found", func() {
		BeforeEach(func() {
			c = fake.NewClientBuilder().Build()
		})

		It("should return an error", func(ctx SpecContext) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("while getting caSecret %s", caSecret.Name)))
		})
	})

	Context("when the ca.crt entry is missing in the secret", func() {
		BeforeEach(func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecret.Name,
					Namespace: caSecret.Namespace,
				},
			}
			c = fake.NewClientBuilder().WithObjects(secret).Build()
		})

		It("should return an error", func(ctx SpecContext) {
			tlsConfig, err := newTLSConfigFromSecret(ctx, c, caSecret)
			Expect(err).To(HaveOccurred())
			Expect(tlsConfig).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("missing %s entry in secret %s", CACertKey, caSecret.Name)))
		})
	})
})
