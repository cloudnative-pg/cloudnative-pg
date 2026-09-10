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

// Package certs handle the PKI infrastructure of the operator
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"slices"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (

	// This is the PEM block type of elliptic courves private key
	ecPrivateKeyPEMBlockType = "EC PRIVATE KEY"

	// This is the PEM block type for certificates
	certificatePEMBlockType = "CERTIFICATE"

	// CACertKey is the key for certificates in a CA secret
	CACertKey = "ca.crt"

	// CAPrivateKeyKey is the key for the private key field in a CA secret
	CAPrivateKeyKey = "ca.key"

	// TLSCertKey is the key for certificates in a CA secret
	TLSCertKey = "tls.crt"

	// TLSPrivateKeyKey is the key for the private key field in a CA secret
	TLSPrivateKeyKey = "tls.key"
)

// CertType represent a certificate type
type CertType string

const (
	// CertTypeClient means a certificate for a client
	CertTypeClient = "client"

	// CertTypeServer means a certificate for a server
	CertTypeServer = "server"
)

// KeyPair represent a pair of keys to be used for asymmetric encryption and a
// certificate declaring the intended usage of those keys
type KeyPair struct {
	// The private key PEM block
	Private []byte

	// The certificate PEM block
	Certificate []byte
}

// ParseECPrivateKey parse the ECDSA private key stored in the pair
func (pair KeyPair) ParseECPrivateKey() (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pair.Private)
	if block == nil || block.Type != ecPrivateKeyPEMBlockType {
		return nil, fmt.Errorf("invalid private key PEM block type")
	}

	return x509.ParseECPrivateKey(block.Bytes)
}

// ParseCertificate parse certificate stored in the pair
func (pair KeyPair) ParseCertificate() (*x509.Certificate, error) {
	block, _ := pem.Decode(pair.Certificate)
	if block == nil || block.Type != certificatePEMBlockType {
		return nil, fmt.Errorf("invalid public key PEM block type")
	}

	return x509.ParseCertificate(block.Bytes)
}

// IsValid checks if given CA and verify options match the server
func (pair KeyPair) IsValid(caPair *KeyPair, opts *x509.VerifyOptions) error {
	if opts == nil {
		opts = &x509.VerifyOptions{}
	}

	blockServer, intermediatesPEM := pem.Decode(pair.Certificate)
	if blockServer == nil || blockServer.Type != certificatePEMBlockType {
		return fmt.Errorf("invalid public key PEM block type")
	}

	serverCert, err := x509.ParseCertificate(blockServer.Bytes)
	if err != nil {
		return err
	}

	if opts.Intermediates == nil {
		opts.Intermediates = x509.NewCertPool()
	}

	// We can ignore the returned bool, because it is perfectly fine
	// not to have an intermediate certificate if the server certificate
	// is directly signed by the CA
	opts.Intermediates.AppendCertsFromPEM(intermediatesPEM)

	if opts.Roots == nil {
		opts.Roots = x509.NewCertPool()
	}

	if !opts.Roots.AppendCertsFromPEM(caPair.Certificate) {
		return fmt.Errorf("invalid CA certificates")
	}

	if _, err = serverCert.Verify(*opts); err != nil {
		return fmt.Errorf("failed to verify certificate: %w", err)
	}
	return nil
}

// CreateAndSignPair given a CA keypair, generate and sign a leaf keypair
func (pair KeyPair) CreateAndSignPair(host string, usage CertType, altDNSNames []string) (*KeyPair, error) {
	certificateDuration := getCertificateDuration()
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)
	return pair.createAndSignPairWithValidity(host, notBefore, notAfter, usage, altDNSNames)
}

func (pair KeyPair) createAndSignPairWithValidity(
	host string,
	notBefore,
	notAfter time.Time,
	usage CertType,
	altDNSNames []string,
) (*KeyPair, error) {
	caCertificate, err := pair.ParseCertificate()
	if err != nil {
		return nil, err
	}

	caPrivateKey, err := pair.ParseECPrivateKey()
	if err != nil {
		return nil, err
	}

	// Generate a new private key
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	// Sign the public part of this key with the CA
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("can't generate serial number: %w", err)
	}

	leafTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		IsCA:                  false,
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames: altDNSNames,
	}

	leafTemplate.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement
	switch usage {
	case CertTypeClient:
		leafTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}

	case CertTypeServer:
		leafTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		leafTemplate.KeyUsage |= x509.KeyUsageKeyEncipherment

		hosts := strings.Split(host, ",")
		for _, h := range hosts {
			if ip := net.ParseIP(h); ip != nil {
				leafTemplate.IPAddresses = append(leafTemplate.IPAddresses, ip)
				continue
			}
			if !slices.Contains(leafTemplate.DNSNames, h) {
				leafTemplate.DNSNames = append(leafTemplate.DNSNames, h)
			}
		}
	}

	certificateBytes, err := x509.CreateCertificate(
		rand.Reader, &leafTemplate, caCertificate, &leafKey.PublicKey, caPrivateKey)
	if err != nil {
		return nil, err
	}

	privateKey, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		Private:     encodePrivateKey(privateKey),
		Certificate: encodeCertificate(certificateBytes),
	}, nil
}

// GenerateCASecret create a k8s CA secret from a key pair
func (pair KeyPair) GenerateCASecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Data: map[string][]byte{
			CAPrivateKeyKey: pair.Private,
			CACertKey:       pair.Certificate,
		},
		Type: corev1.SecretTypeOpaque,
	}
}

// GenerateCertificateSecret creates a k8s server secret from a key pair
func (pair KeyPair) GenerateCertificateSecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Data: map[string][]byte{
			TLSPrivateKeyKey: pair.Private,
			TLSCertKey:       pair.Certificate,
		},
		Type: corev1.SecretTypeTLS,
	}
}

// GenerateWebhookCertificateSecret creates a k8s webhook secret from a key pair including the CA
// This method is specifically for webhook certificates that need the CA bundle for validation
func (pair KeyPair) GenerateWebhookCertificateSecret(namespace, name string, caCertificate []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Data: map[string][]byte{
			TLSPrivateKeyKey: pair.Private,
			TLSCertKey:       pair.Certificate,
			CACertKey:        caCertificate,
		},
		Type: corev1.SecretTypeTLS,
	}
}

// RenewCertificate create a new certificate for the embedded private
// key, replacing the existing one. The certificate will be signed
// with the passed private key and will have as parent the specified
// parent certificate. If the parent certificate is nil the certificate
// will be self-signed
func (pair *KeyPair) RenewCertificate(
	caPrivateKey *ecdsa.PrivateKey,
	parentCertificate *x509.Certificate,
	altDNSNames []string,
) error {
	oldCertificate, err := pair.ParseCertificate()
	if err != nil {
		return err
	}

	certificateDuration := getCertificateDuration()
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	newCertificate := *oldCertificate
	newCertificate.NotBefore = notBefore
	newCertificate.NotAfter = notAfter
	newCertificate.SerialNumber = serialNumber
	newCertificate.DNSNames = altDNSNames

	if parentCertificate == nil {
		parentCertificate = &newCertificate
	}

	tlsPrivateKey, err := pair.ParseECPrivateKey()
	if err != nil {
		return err
	}

	certificateBytes, err := x509.CreateCertificate(
		rand.Reader,
		&newCertificate,
		parentCertificate,
		&tlsPrivateKey.PublicKey,
		caPrivateKey)
	if err != nil {
		return err
	}

	pair.Certificate = encodeCertificate(certificateBytes)
	return nil
}

// IsExpiring check if the certificate will expire in the configured duration
func (pair *KeyPair) IsExpiring() (bool, *time.Time, error) {
	cert, err := pair.ParseCertificate()
	if err != nil {
		return true, nil, err
	}

	if time.Now().Before(cert.NotBefore) {
		return true, &cert.NotAfter, nil
	}
	expiringCheckThreshold := getCheckThreshold()
	if time.Now().Add(expiringCheckThreshold).After(cert.NotAfter) {
		return true, &cert.NotAfter, nil
	}

	return false, &cert.NotAfter, nil
}

// DoAltDNSNamesMatch checks if the certificate has all of the specified altDNSNames
func (pair *KeyPair) DoAltDNSNamesMatch(altDNSNames []string) (bool, error) {
	cert, err := pair.ParseCertificate()
	if err != nil {
		return false, err
	}

	sort.Strings(cert.DNSNames)
	sort.Strings(altDNSNames)

	return slices.Equal(cert.DNSNames, altDNSNames), nil
}

// CreateDerivedCA create a new CA derived from the certificate in the
// keypair
func (pair *KeyPair) CreateDerivedCA(commonName string, organizationalUnit string) (*KeyPair, error) {
	certificate, err := pair.ParseCertificate()
	if err != nil {
		return nil, err
	}

	key, err := pair.ParseECPrivateKey()
	if err != nil {
		return nil, err
	}

	certificateDuration := getCertificateDuration()
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)

	return createCAWithValidity(notBefore, notAfter, certificate, key, commonName, organizationalUnit)
}

// CreateRootCA generates a CA returning its keys
func CreateRootCA(commonName string, organizationalUnit string) (*KeyPair, error) {
	certificateDuration := getCertificateDuration()
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)
	return createCAWithValidity(notBefore, notAfter, nil, nil, commonName, organizationalUnit)
}

// ParseCASecret parse a CA secret to a key pair
func ParseCASecret(secret *corev1.Secret) (*KeyPair, error) {
	privateKey, ok := secret.Data[CAPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("missing %s secret data", CAPrivateKeyKey)
	}

	publicKey, ok := secret.Data[CACertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s secret data", CACertKey)
	}

	// Verify the key matches the certificate
	_, err := tls.X509KeyPair(publicKey, privateKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		Private:     privateKey,
		Certificate: publicKey,
	}, nil
}

// ParseServerSecret parse a secret for a server to a key pair
func ParseServerSecret(secret *corev1.Secret) (*KeyPair, error) {
	privateKey, ok := secret.Data[TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("missing %v secret data", TLSPrivateKeyKey)
	}

	publicKey, ok := secret.Data[TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s secret data", TLSCertKey)
	}

	// Verify the key matches the certificate
	_, err := tls.X509KeyPair(publicKey, privateKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		Private:     privateKey,
		Certificate: publicKey,
	}, nil
}

// createCAWithValidity create a CA with a certain validity, with a parent certificate and signed by a certain
// private key. If the latest two parameters are nil, the CA will be a root one (self-signed)
func createCAWithValidity(
	notBefore,
	notAfter time.Time,
	parentCertificate *x509.Certificate,
	parentPrivateKey interface{},
	commonName string,
	organizationalUnit string,
) (*KeyPair, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	rootTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		Subject: pkix.Name{
			CommonName: commonName,
			OrganizationalUnit: []string{
				organizationalUnit,
			},
		},
	}

	if parentCertificate == nil {
		parentCertificate = &rootTemplate
	}

	if parentPrivateKey == nil {
		parentPrivateKey = rootKey
	}

	certificateBytes, err := x509.CreateCertificate(
		rand.Reader,
		&rootTemplate,
		parentCertificate,
		&rootKey.PublicKey,
		parentPrivateKey)
	if err != nil {
		return nil, err
	}

	privateKey, err := x509.MarshalECPrivateKey(rootKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		Private:     encodePrivateKey(privateKey),
		Certificate: encodeCertificate(certificateBytes),
	}, nil
}

func encodeCertificate(derBytes []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}

func encodePrivateKey(derBytes []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: ecPrivateKeyPEMBlockType, Bytes: derBytes})
}

func getCertificateDuration() time.Duration {
	duration := configuration.Current.CertificateDuration
	if duration <= 0 {
		return configuration.CertificateDuration * 24 * time.Hour
	}
	return time.Duration(duration) * 24 * time.Hour
}

func getCheckThreshold() time.Duration {
	threshold := configuration.Current.ExpiringCheckThreshold
	if threshold <= 0 {
		return configuration.ExpiringCheckThreshold * 24 * time.Hour
	}
	return time.Duration(threshold) * 24 * time.Hour
}
