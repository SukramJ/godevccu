// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// TLSConfig enables the HTTPS twins of the API ports. A real CCU serves
// every remote API on a plaintext port and a TLS port at once —
// 2001/42001 for BidCos-RF, 2010/42010 for HmIP-RF, 9292/49292 for the
// group interface, 80/443 for the web API — and clients decide per
// connection which to use.
//
// The zero value leaves TLS off, which is what pydevccu does.
type TLSConfig struct {
	// Enabled turns the TLS listeners on.
	Enabled bool

	// CertPEM and KeyPEM supply a certificate. When either is empty a
	// self-signed one is generated at startup for localhost, 127.0.0.1
	// and ::1 — enough for a client that skips verification, which is
	// what a client talking to a real CCU's factory certificate does
	// anyway.
	CertPEM []byte
	KeyPEM  []byte

	// XMLRPCPort and JSONRPCPort override the TLS ports. 0 follows the
	// CCU convention: the plaintext XML-RPC port + 40000, and 443 for
	// the web API. Accepts [EphemeralPort].
	XMLRPCPort  int
	JSONRPCPort int

	// Redirect makes the plaintext web API answer 302 towards its HTTPS
	// twin and CCU.getHttpsRedirectEnabled report true, modelling a CCU
	// configured to enforce HTTPS.
	Redirect bool
}

// tlsPortOffset is the distance between a CCU's plaintext RPC port and
// its TLS twin (2001 → 42001).
const tlsPortOffset = 40000

// generateSelfSigned returns a PEM certificate/key pair valid for the
// loopback names a simulator is reached under.
func generateSelfSigned() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("virtualccu: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("virtualccu: serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"godevccu"},
			CommonName:   "godevccu",
		},
		NotBefore: time.Now().Add(-time.Hour),
		// 397 days is the maximum lifetime current TLS stacks accept
		// for a server certificate; anything longer is rejected as
		// non-compliant before the self-signed check even runs.
		NotAfter:              time.Now().AddDate(0, 0, 397),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost", "godevccu"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("virtualccu: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("virtualccu: marshal key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// resolve returns the certificate material to serve, generating a
// self-signed pair when none was supplied.
func (t TLSConfig) resolve() (certPEM, keyPEM []byte, err error) {
	if len(t.CertPEM) > 0 && len(t.KeyPEM) > 0 {
		return t.CertPEM, t.KeyPEM, nil
	}
	return generateSelfSigned()
}

// xmlRPCPort returns the TLS port for the XML-RPC surface given the
// plaintext port in use.
func (t TLSConfig) xmlRPCPort(plaintext int) int {
	if t.XMLRPCPort != 0 {
		return t.XMLRPCPort
	}
	if plaintext <= 0 {
		return EphemeralPort
	}
	return plaintext + tlsPortOffset
}

// jsonRPCPort returns the TLS port for the web API.
func (t TLSConfig) jsonRPCPort() int {
	if t.JSONRPCPort != 0 {
		return t.JSONRPCPort
	}
	return 443
}
