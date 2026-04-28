// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package pki implements the controller's mini-CA and enrollment
// flow. Agents and relays bootstrap with a one-shot enrollment
// token, then submit a CSR to receive a short-lived X.509 cert that
// carries their identity URI as a SAN. The CA is a single trust
// domain; cross-trust-domain federation is out of scope.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"

	"github.com/boanlab/OutRelay/lib/identity"
)

const (
	// DefaultLeafTTL is the default validity of issued agent/relay certs.
	// Short on purpose — revocation is handled by TTL, not CRL/OCSP.
	DefaultLeafTTL = time.Hour

	// caTTL is the self-signed CA validity.
	caTTL = 10 * 365 * 24 * time.Hour
)

var (
	ErrCSRSignature   = errors.New("pki: csr signature invalid")
	ErrCSRMissingURI  = errors.New("pki: csr missing identity URI SAN")
	ErrCSRURIMismatch = errors.New("pki: csr URI SAN does not match issued name")
)

// CA holds the controller's signing material.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certDER []byte
}

// NewCA generates a fresh self-signed CA (ECDSA P-256, 10-year TTL).
func NewCA() (*CA, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate ca key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "outrelay-ca"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(caTTL),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("pki: create ca cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: priv, certDER: der}, nil
}

// Sign issues a leaf cert for the given Name. The CSR must be self-
// signed (proves possession of the private key) and must contain the
// expected URI SAN — the controller refuses to sign a CSR for a name
// the requester didn't claim. If ttl is non-positive, DefaultLeafTTL
// is used.
func (ca *CA) Sign(csrDER []byte, name identity.Name, ttl time.Duration) ([]byte, error) {
	if ttl <= 0 {
		ttl = DefaultLeafTTL
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("pki: parse csr: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCSRSignature, err)
	}
	expected := name.String()
	if !hasURI(csr.URIs, expected) {
		if len(csr.URIs) == 0 {
			return nil, ErrCSRMissingURI
		}
		return nil, ErrCSRURIMismatch
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: name.String(),
		},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(ttl),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{name.URL()},
		DNSNames:    []string{"localhost"}, // dev convenience; relay LB hostname overrides in production
	}
	return x509.CreateCertificate(rand.Reader, &tmpl, ca.cert, csr.PublicKey, ca.key)
}

// CertPEM returns the CA cert in PEM form (safe to distribute).
func (ca *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.certDER})
}

// KeyPEM returns the CA private key in PEM form (sensitive).
func (ca *CA) KeyPEM() ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(ca.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// CertPool returns a *x509.CertPool with this CA as the only root.
// Use as RootCAs (clients verifying server) or ClientCAs (server
// verifying client) for mTLS.
func (ca *CA) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return pool
}

// NewCSR generates a fresh ECDSA P-256 keypair and a CSR carrying the
// given Name as a URI SAN. The caller (agent or relay) retains the
// returned key.
func NewCSR(name identity.Name) (csrDER []byte, key *ecdsa.PrivateKey, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: generate leaf key: %w", err)
	}
	tmpl := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: name.String()},
		URIs:    []*url.URL{name.URL()},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &tmpl, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: create csr: %w", err)
	}
	return der, priv, nil
}

func randomSerial() (*big.Int, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	return n, nil
}

func hasURI(uris []*url.URL, want string) bool {
	for _, u := range uris {
		if u.String() == want {
			return true
		}
	}
	return false
}
