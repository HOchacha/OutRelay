// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package identity_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/identity"
	"github.com/boanlab/OutRelay/lib/orp"
	"github.com/boanlab/OutRelay/lib/transport"
	"github.com/boanlab/OutRelay/pkg/pki"
)

// TestEnrollToMTLSHandshake exercises the full bootstrap path end to
// end: enrollment token → CSR → controller signs → agent and relay
// each receive a leaf cert → mTLS QUIC handshake → ORP frame
// round-trip across the secured channel.
//
// The flow models a real Agent connecting to a Relay, with the
// Controller standing in as the issuer that signs both parties'
// CSRs.
func TestEnrollToMTLSHandshake(t *testing.T) {
	t.Parallel()

	// 1. Controller bootstraps: CA + Enroller.
	ca, err := pki.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	enroller, err := pki.NewEnroller()
	if err != nil {
		t.Fatal(err)
	}

	// 2. Operator issues an enrollment token to the agent (out-of-band).
	agentToken, err := enroller.Issue("acme", 0)
	if err != nil {
		t.Fatal(err)
	}
	relayToken, err := enroller.Issue("acme", 0)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Each party performs enrollment → CSR → cert.
	agentTLS := enroll(t, enroller, ca, "acme", agentToken)
	relayTLS := enroll(t, enroller, ca, "acme", relayToken)

	// 4. Set up a relay-side TLS config requiring client certs and
	// an agent-side config trusting the CA.
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{*relayTLS},
		ClientCAs:    ca.CertPool(),
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{*agentTLS},
		RootCAs:      ca.CertPool(),
		ServerName:   "localhost",
	}

	// 5. Start a QUIC listener and dial it. Verify mTLS succeeds and
	// an ORP frame survives the round-trip.
	ln, err := transport.ListenQUIC("127.0.0.1:0", serverTLS, nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	type result struct {
		f   *orp.Frame
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := ln.Accept(ctx)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer c.Close()
		s, err := c.AcceptStream(ctx)
		if err != nil {
			done <- result{err: err}
			return
		}
		f, err := orp.ParseFrame(s)
		done <- result{f: f, err: err}
	}()

	c, err := transport.DialQUIC(ctx, ln.Addr().String(), clientTLS, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	s, err := c.OpenStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := &orp.Frame{
		Version: orp.Version1,
		Type:    orp.FrameTypeHello,
		Payload: []byte("after-mtls"),
	}
	wire, _ := want.MarshalBinary()
	if _, err := s.Write(wire); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("server: %v", res.err)
	}
	if res.f.Type != orp.FrameTypeHello || string(res.f.Payload) != "after-mtls" {
		t.Fatalf("unexpected frame: %+v", res.f)
	}
}

// enroll runs the full token → CSR → signed cert flow and returns a
// tls.Certificate ready for use in tls.Config.Certificates.
func enroll(t *testing.T, e *pki.Enroller, ca *pki.CA, tenant, token string) *tls.Certificate {
	t.Helper()
	claims, err := e.Verify(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.Tenant != tenant {
		t.Fatalf("tenant mismatch: %s", claims.Tenant)
	}
	name, err := identity.NewAgent(claims.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, key, err := pki.NewCSR(name)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := ca.Sign(csrDER, name, 0)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: key, Leaf: leaf}
}
