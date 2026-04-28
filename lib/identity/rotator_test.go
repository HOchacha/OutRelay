// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package identity_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/identity"
)

// TestRotatorBootstrapAndCurrent verifies the basic store/load.
func TestRotatorBootstrapAndCurrent(t *testing.T) {
	t.Parallel()
	c := makeCert(t, time.Now(), time.Now().Add(time.Hour))
	r := identity.NewRotator(func(ctx context.Context) (*identity.Cert, error) {
		return c, nil
	})
	if r.Current() != nil {
		t.Fatal("Current before Bootstrap should be nil")
	}
	if err := r.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.Current() != c {
		t.Fatal("Current after Bootstrap mismatch")
	}
}

// TestRotatorRefreshes drives a short-TTL cert through the rotator's
// refresh path and observes Subscribe receiving the new cert.
func TestRotatorRefreshes(t *testing.T) {
	t.Parallel()
	now := time.Now()
	first := makeCert(t, now, now.Add(200*time.Millisecond))
	second := makeCert(t, now, now.Add(time.Hour))

	var calls atomic.Int32
	r := identity.NewRotator(func(ctx context.Context) (*identity.Cert, error) {
		switch calls.Add(1) {
		case 1:
			return first, nil
		default:
			return second, nil
		}
	})
	// rotateAt=0.1 means refresh after 10% of TTL, i.e. ~20ms after issue.
	if err := r.SetRotateAt(0.1); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	sub := r.Subscribe()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	select {
	case got := <-sub:
		if got != second {
			t.Fatal("subscriber got wrong cert")
		}
		if r.Current() != second {
			t.Fatal("Current did not update")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for refresh")
	}
}

// TestRotatorRunWithoutBootstrap returns an error.
func TestRotatorRunWithoutBootstrap(t *testing.T) {
	t.Parallel()
	r := identity.NewRotator(func(ctx context.Context) (*identity.Cert, error) {
		return nil, errors.New("nope")
	})
	if err := r.Run(t.Context()); err == nil {
		t.Fatal("Run without Bootstrap should error")
	}
}

// makeCert produces a tls.Certificate + parsed x509.Certificate with
// the given NotBefore/NotAfter. The cert chain is irrelevant for
// rotator unit tests — we only care about the timing fields.
func makeCert(t *testing.T, notBefore, notAfter time.Time) *identity.Cert {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rotator-test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &identity.Cert{
		TLS:  &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: parsed},
		X509: parsed,
	}
}
