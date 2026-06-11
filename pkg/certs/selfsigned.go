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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// GenerateSelfSignedClientCertificate generates an in-memory self-signed ECDSA P-256
// certificate with ExtKeyUsageClientAuth. The certificate is never written to disk.
func GenerateSelfSignedClientCertificate(commonName string) (*KeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("can't generate serial number: %w", err)
	}

	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := notBefore.Add(getCertificateDuration())

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	privateKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		Private:     encodePrivateKey(privateKey),
		Certificate: encodeCertificate(certBytes),
	}, nil
}

// TLSCertificate converts the key pair to a tls.Certificate.
func (pair KeyPair) TLSCertificate() (tls.Certificate, error) {
	return tls.X509KeyPair(pair.Certificate, pair.Private)
}

// PublicKeyFingerprint returns the hex-encoded SHA-256 digest of the certificate's
// SubjectPublicKeyInfo, which is stable across certificate renewals that reuse the
// same key.
func PublicKeyFingerprint(cert *x509.Certificate) string {
	digest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(digest[:])
}
