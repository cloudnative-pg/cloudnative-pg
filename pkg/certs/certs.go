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
)

const (
	// This is the lifetime of the generated certificates
	certificateDuration = 365 * 24 * time.Hour

	// This is the PEM block type of elliptic courves private key
	ecPrivateKeyPEMBlockType = "EC PRIVATE KEY"

	// This is the PEM block type for certificates
	certificatePEMBlockType = "CERTIFICATE"
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
		return nil, fmt.Errorf("invalid private key PEM block type")
	}

	return x509.ParseCertificate(block.Bytes)
}

// CreateAndSignPair given a CA keypair, generate and sign a leaf keypair
func (pair KeyPair) CreateAndSignPair(host string) (*KeyPair, error) {
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

	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)
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

// CreateCA generates a CA returning its keys
func CreateCA() (*KeyPair, error) {
	notBefore := time.Now().Add(time.Minute * -5)
	notAfter := notBefore.Add(certificateDuration)

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
