// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package transport_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/orp"
	"github.com/boanlab/OutRelay/lib/transport"
)

// TestQUICRoundtrip exercises the full transport stack: listen → dial →
// open stream → write ORP frame → read ORP frame → match. This locks
// the integration between lib/transport and lib/orp before subsequent
// phases build on top.
func TestQUICRoundtrip(t *testing.T) {
	t.Parallel()

	serverTLS, clientTLS := selfSignedTLS(t)

	ln, err := transport.ListenQUIC("127.0.0.1:0", serverTLS, nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Server: accept conn, accept stream, parse frame, echo.
	type result struct {
		f   *orp.Frame
		err error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer conn.Close()
		s, err := conn.AcceptStream(ctx)
		if err != nil {
			done <- result{err: err}
			return
		}
		f, err := orp.ParseFrame(s)
		done <- result{f: f, err: err}
	}()

	// Client: dial, open stream, write a HELLO frame.
	conn, err := transport.DialQUIC(ctx, ln.Addr().String(), clientTLS, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	s, err := conn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	want := &orp.Frame{
		Version: orp.Version1,
		Type:    orp.FrameTypeHello,
		Flags:   0x1234,
		Payload: []byte("hello-from-test"),
	}
	data, err := want.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := s.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("server: %v", res.err)
	}
	got := res.f
	if got.Version != want.Version || got.Type != want.Type || got.Flags != want.Flags {
		t.Fatalf("header mismatch: got %+v want %+v", got, want)
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload mismatch: got %q want %q", got.Payload, want.Payload)
	}
}

// selfSignedTLS produces a server tls.Config with a self-signed cert and
// a matching client tls.Config that trusts that cert. ALPN is left
// unset so transport.ensureALPN exercises its append path.
func selfSignedTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "outrelay-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("append cert")
	}

	server = &tls.Config{Certificates: []tls.Certificate{cert}}
	client = &tls.Config{RootCAs: pool, ServerName: "localhost"}
	return server, client
}
