/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package certs handle the PKI infrastructure of the operator
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// This is the lifetime of the generated certificates
	certificateDuration = 365 * 24 * time.Hour

	// This is the PEM block type of elliptic courves private key
	ecPrivateKeyPEMBlockType = "EC PRIVATE KEY"

	// This is the PEM block type for certificates
	certificatePEMBlockType = "CERTIFICATE"

	// Threshold to consider a certificate as expiring
	expiringCheckThreshold = 7 * 24 * time.Hour
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

// CreateAndSignPair given a CA keypair, generate and sign a leaf keypair
func (pair KeyPair) CreateAndSignPair(host string) (*KeyPair, error) {
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)
	return pair.createAndSignPairWithValidity(host, notBefore, notAfter)
}

func (pair KeyPair) createAndSignPairWithValidity(host string, notBefore, notAfter time.Time) (*KeyPair, error) {
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
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	hosts := strings.Split(host, ",")
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			leafTemplate.IPAddresses = append(leafTemplate.IPAddresses, ip)
		} else {
			leafTemplate.DNSNames = append(leafTemplate.DNSNames, h)
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
func (pair KeyPair) GenerateCASecret(namespace, name string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"ca.key": pair.Private,
			"ca.crt": pair.Certificate,
		},
		Type: v1.SecretTypeOpaque,
	}
}

// GenerateServerSecret create a k8s server secret from a key pair
func (pair KeyPair) GenerateServerSecret(namespace, name string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"tls.key": pair.Private,
			"tls.crt": pair.Certificate,
		},
		Type: v1.SecretTypeTLS,
	}
}

// RenewCertificate create a new certificate for the embedded private
// key, replacing the existing one. The certificate will be signed
// with the passed private key and will have as parent the specified
// parent certificate. If the parent certificate is nil the certificate
// will be self-signed
func (pair *KeyPair) RenewCertificate(caPrivateKey *ecdsa.PrivateKey, parentCertificate *x509.Certificate) error {
	oldCertificate, err := pair.ParseCertificate()
	if err != nil {
		return err
	}

	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)

	newCertificate := *oldCertificate
	newCertificate.NotBefore = notBefore
	newCertificate.NotAfter = notAfter

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	rootTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if parentCertificate == nil {
		parentCertificate = &rootTemplate
	}

	certificateBytes, err := x509.CreateCertificate(
		rand.Reader,
		&rootTemplate,
		parentCertificate,
		&caPrivateKey.PublicKey,
		caPrivateKey)
	if err != nil {
		return err
	}

	pair.Certificate = encodeCertificate(certificateBytes)
	return nil
}

// IsExpiring check if the certificate will expire in the configured duration
func (pair *KeyPair) IsExpiring() (bool, error) {
	cert, err := pair.ParseCertificate()
	if err != nil {
		return true, err
	}

	if time.Now().Before(cert.NotBefore) {
		return true, nil
	}
	if time.Now().Add(expiringCheckThreshold).After(cert.NotAfter) {
		return true, nil
	}

	return false, nil
}

// CreateCA generates a CA returning its keys
func CreateCA() (*KeyPair, error) {
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)
	return createCAWithValidity(notBefore, notAfter)
}

// ParseCASecret parse a CA secret to a key pair
func ParseCASecret(secret *v1.Secret) (*KeyPair, error) {
	privateKey, ok := secret.Data["ca.key"]
	if !ok {
		return nil, fmt.Errorf("missing ca.key secret data")
	}

	publicKey, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, fmt.Errorf("missing ca.crt secret data")
	}

	return &KeyPair{
		Private:     privateKey,
		Certificate: publicKey,
	}, nil
}

// ParseServerSecret parse a secret for a server to a key pair
func ParseServerSecret(secret *v1.Secret) (*KeyPair, error) {
	privateKey, ok := secret.Data["tls.key"]
	if !ok {
		return nil, fmt.Errorf("missing tls.key secret data")
	}

	publicKey, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("missing tls.crt secret data")
	}

	return &KeyPair{
		Private:     privateKey,
		Certificate: publicKey,
	}, nil
}

func createCAWithValidity(notBefore, notAfter time.Time) (*KeyPair, error) {
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
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certificateBytes, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, &rootKey.PublicKey, rootKey)
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
