// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package transport_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/lib/transport"
)

// TestTCPDialListenRoundTrip stands up a TCP+TLS+yamux listener,
// dials it, opens a stream from the client, accepts it server-side,
// echoes one message in each direction, and tears down. Mirrors
// quic_test.go's basic round-trip but exercises the TCP fallback path.
func TestTCPDialListenRoundTrip(t *testing.T) {
	t.Parallel()
	server, client := localTLSPair(t)

	ln, err := transport.ListenTCP("127.0.0.1:0", server)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var serverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept(ctx)
		if err != nil {
			serverErr = err
			return
		}
		defer conn.Close()
		s, err := conn.AcceptStream(ctx)
		if err != nil {
			serverErr = err
			return
		}
		buf := make([]byte, 16)
		n, err := io.ReadFull(s, buf[:5])
		if err != nil {
			serverErr = err
			return
		}
		if string(buf[:n]) != "hello" {
			serverErr = io.ErrUnexpectedEOF
			return
		}
		if _, err := s.Write([]byte("world")); err != nil {
			serverErr = err
			return
		}
		_ = s.Close()
	}()

	conn, err := transport.DialTCP(ctx, addr, client)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	s, err := conn.OpenStream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := s.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := make([]byte, 5)
	if _, err := io.ReadFull(s, resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(resp) != "world" {
		t.Fatalf("got %q want %q", resp, "world")
	}

	wg.Wait()
	if serverErr != nil {
		t.Fatalf("server side: %v", serverErr)
	}
}

// localTLSPair generates a one-shot self-signed cert and returns
// (server tlsConf, client tlsConf) wired so client validates the
// server against the pinned cert. mTLS with a separate CA is
// already exercised by quic_test.go; this test focuses purely on
// the TCP transport plumbing so we keep TLS minimal.
func localTLSPair(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	pool := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(parsed)

	server := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	client := &tls.Config{
		RootCAs:    pool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
	}
	return server, client
}
