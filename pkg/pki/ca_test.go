// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package pki_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/identity"
	"github.com/boanlab/OutRelay/pkg/pki"
)

// TestCASignAndVerify: a CSR for a Name signed by the CA produces a
// cert that chains back to the CA pool with the URI SAN intact.
func TestCASignAndVerify(t *testing.T) {
	t.Parallel()
	ca, err := pki.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	name, err := identity.NewAgent("acme")
	if err != nil {
		t.Fatal(err)
	}
	csrDER, _, err := pki.NewCSR(name)
	if err != nil {
		t.Fatal(err)
	}

	leafDER, err := ca.Sign(csrDER, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       ca.CertPool(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		CurrentTime: time.Now(),
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != name.String() {
		t.Fatalf("URI SAN mismatch: %v", leaf.URIs)
	}
	if got := leaf.NotAfter.Sub(leaf.NotBefore); got < pki.DefaultLeafTTL {
		t.Fatalf("leaf TTL %s too short", got)
	}
}

// TestCASignRejectsBadURI: signing a CSR whose URI SAN doesn't match
// the requested Name must fail. Otherwise an attacker who steals an
// enrollment token could craft a CSR for an arbitrary identity.
func TestCASignRejectsBadURI(t *testing.T) {
	t.Parallel()
	ca, _ := pki.NewCA()

	mallory, _ := identity.NewAgent("acme")
	victim, _ := identity.NewAgent("acme")

	csrDER, _, _ := pki.NewCSR(mallory)
	if _, err := ca.Sign(csrDER, victim, 0); !errors.Is(err, pki.ErrCSRURIMismatch) {
		t.Fatalf("got %v, want ErrCSRURIMismatch", err)
	}
}

// TestCASignRejectsMissingURI: CSR with no URI SAN at all is rejected.
func TestCASignRejectsMissingURI(t *testing.T) {
	t.Parallel()
	ca, _ := pki.NewCA()
	name, _ := identity.NewAgent("acme")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: "no-uri"}}, priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Sign(csrDER, name, 0); !errors.Is(err, pki.ErrCSRMissingURI) {
		t.Fatalf("got %v, want ErrCSRMissingURI", err)
	}
}

// TestCASignDetectsTamperedCSR: a CSR whose body is tampered must be
// rejected by the signature check.
func TestCASignDetectsTamperedCSR(t *testing.T) {
	t.Parallel()
	ca, _ := pki.NewCA()
	name, _ := identity.NewAgent("acme")
	csrDER, _, _ := pki.NewCSR(name)

	tampered := append([]byte(nil), csrDER...)
	tampered[50] ^= 0x01 // bit inside TBSCertificateRequest
	if _, err := ca.Sign(tampered, name, 0); err == nil {
		t.Fatal("expected error on tampered csr")
	}
}
