package tls

import (
	"bytes"
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
	"time"
)

// TLSConfig is adapted from http.Server.ServeTLS
func TLSConfig(hosts []string) (*tls.Config, error) {
	certPEMBlock, keyPEMBlock, err := generateKeyPair(hosts)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate TLS keys %w", err)
	}

	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate X509 key pair %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	return tlsConfig, nil
}

// Adapted from https://go.dev/src/crypto/tls/generate_cert.go
func generateKeyPair(hosts []string) ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("Failing to generate new ecdsa key: %w", err)
	}

	// A CA is supposed to choose unique serial numbers, that is, unique for the CA.
	maxSerialNumber := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, maxSerialNumber)

	if err != nil {
		return nil, nil, fmt.Errorf("Failed to generate a random serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Weaveworks"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 365),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create certificate: %w", err)
	}

	certPEMBlock := &bytes.Buffer{}

	err = pem.Encode(certPEMBlock, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to encode cert pem: %w", err)
	}

	keyPEMBlock := &bytes.Buffer{}

	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to marshal ECDSA private key: %v", err)
	}

	err = pem.Encode(keyPEMBlock, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to encode key pem: %w", err)
	}

	return certPEMBlock.Bytes(), keyPEMBlock.Bytes(), nil
}
