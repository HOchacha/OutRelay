// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// TCP+TLS transport using yamux for stream multiplexing — the
// fallback path for environments where UDP/QUIC is blocked
// (egress firewalls, captive networks, some CSP egress policies).
// Behaves like the QUIC transport from the rest of the codebase's
// perspective: each Conn supports OpenStream / AcceptStream and
// each Stream is a bidirectional byte stream with optional
// CloseWrite. mTLS is layered between the raw TCP socket and the
// yamux session so identity is verified the same way as on QUIC.
//
// yamux gives us flow control, half-close (CloseWrite), and a
// stream-id space — close to QUIC's stream model. It does NOT
// expose a per-stream CancelRead, so our Stream.CancelRead falls
// back to a full Close (which the local Read observes as EOF).
// That's a coarser hammer than QUIC's STOP_SENDING but it still
// unblocks parked Reads — the SwapInner path uses CancelRead purely
// as a "wake up and re-read on the new inner" signal, so EOF is an
// acceptable substitution.

package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/quic-go/quic-go"
)

// TCPDialer satisfies Dialer for the TCP+TLS+yamux fallback path.
// quic.Config is ignored — TCP has no equivalent knobs in the
// places we use them. Pass this into Session.DialWithDialer when
// the agent is in UDP-blocked mode.
type TCPDialer struct{}

func (TCPDialer) Dial(ctx context.Context, addr string, tlsConf *tls.Config, _ *quic.Config) (Conn, error) {
	return DialTCP(ctx, addr, tlsConf)
}

// DialTCP opens a TCP+TLS connection to addr and wraps it in a
// yamux client session. Returns a Conn whose OpenStream / AcceptStream
// produce yamux streams.
//
// tlsConf must contain a client cert (mTLS) and a CA pool. ALPN is
// stamped (same value used for QUIC) so a relay can serve both
// QUIC and TCP+TLS without ambiguity at the TLS layer.
func DialTCP(ctx context.Context, addr string, tlsConf *tls.Config) (Conn, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	tlsConf = ensureALPN(tlsConf.Clone())

	d := &net.Dialer{}
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: tcp dial %s: %w", addr, err)
	}
	tlsConn := tls.Client(rawConn, tlsConf)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("transport: tcp+tls handshake %s: %w", addr, err)
	}
	sess, err := yamux.Client(tlsConn, yamuxConfig())
	if err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("transport: yamux client: %w", err)
	}
	return &yamuxConn{sess: sess, underlying: tlsConn}, nil
}

// ListenTCP opens a TCP listener at addr that wraps each accepted
// TCP connection in TLS + a yamux server session. The returned
// Listener.Accept yields one yamuxConn per TCP+TLS connection.
//
// Use this alongside ListenQUIC on a relay to accept agents that
// can't reach UDP/443. mTLS uses the same CA pool as the QUIC path.
func ListenTCP(addr string, tlsConf *tls.Config) (Listener, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	tlsConf = ensureALPN(tlsConf.Clone())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: tcp listen %s: %w", addr, err)
	}
	return &yamuxListener{
		ln:      ln,
		tlsConf: tlsConf,
	}, nil
}

// yamuxConfig tweaks a couple defaults that bite OutRelay's traffic
// pattern. Specifically: disable yamux's built-in 30s keepalive
// (we drive keepalive at the ORP layer) and bump the idle stream
// window so a chatty stream doesn't stall on yamux's flow control.
func yamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	return cfg
}

// yamuxListener wraps net.Listener so each accepted TCP connection
// completes a TLS handshake and starts a yamux server session
// before being surfaced to the caller as a Conn.
type yamuxListener struct {
	ln      net.Listener
	tlsConf *tls.Config
}

func (l *yamuxListener) Accept(ctx context.Context) (Conn, error) {
	type result struct {
		c   Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		rawConn, err := l.ln.Accept()
		if err != nil {
			ch <- result{err: err}
			return
		}
		tlsConn := tls.Server(rawConn, l.tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			ch <- result{err: fmt.Errorf("transport: tcp+tls handshake: %w", err)}
			return
		}
		sess, err := yamux.Server(tlsConn, yamuxConfig())
		if err != nil {
			_ = tlsConn.Close()
			ch <- result{err: fmt.Errorf("transport: yamux server: %w", err)}
			return
		}
		ch <- result{c: &yamuxConn{sess: sess, underlying: tlsConn}}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-ctx.Done():
		_ = l.ln.Close()
		return nil, ctx.Err()
	}
}

func (l *yamuxListener) Addr() net.Addr { return l.ln.Addr() }
func (l *yamuxListener) Close() error   { return l.ln.Close() }

// yamuxConn satisfies transport.Conn over a yamux session.
type yamuxConn struct {
	sess       *yamux.Session
	underlying net.Conn // *tls.Conn
}

func (c *yamuxConn) OpenStream(ctx context.Context) (Stream, error) {
	type result struct {
		s   *yamux.Stream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := c.sess.OpenStream()
		ch <- result{s: s, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return &yamuxStream{s: r.s}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *yamuxConn) AcceptStream(ctx context.Context) (Stream, error) {
	type result struct {
		s   *yamux.Stream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := c.sess.AcceptStream()
		ch <- result{s: s, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return &yamuxStream{s: r.s}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *yamuxConn) LocalAddr() net.Addr  { return c.underlying.LocalAddr() }
func (c *yamuxConn) RemoteAddr() net.Addr { return c.underlying.RemoteAddr() }

func (c *yamuxConn) TLS() tls.ConnectionState {
	if tc, ok := c.underlying.(*tls.Conn); ok {
		return tc.ConnectionState()
	}
	return tls.ConnectionState{}
}

func (c *yamuxConn) Close() error { return c.sess.Close() }

// yamuxStream satisfies transport.Stream over a yamux stream.
type yamuxStream struct {
	s *yamux.Stream
}

func (s *yamuxStream) Read(p []byte) (int, error)  { return s.s.Read(p) }
func (s *yamuxStream) Write(p []byte) (int, error) { return s.s.Write(p) }
func (s *yamuxStream) Close() error                { return s.s.Close() }

// yamux uses uint32 stream ids; we widen for the transport.Stream
// interface, which carries them as uint64 (matching QUIC).
func (s *yamuxStream) StreamID() uint64 { return uint64(s.s.StreamID()) }

// CancelRead has no native yamux equivalent; close the whole stream
// so the local Read returns and the SwapInner path can move on to
// the freshly installed inner. This also reaches the peer as a
// stream close, which is harsher than QUIC's STOP_SENDING but keeps
// the same semantics for our consumers.
func (s *yamuxStream) CancelRead(_ uint64) {
	_ = s.s.Close()
}

// We deliberately do NOT implement CloseWrite. yamux's stream Close
// is a full close, not a half-close, so the bridge's
// halfCloseWrite type assertion against `interface{ CloseWrite() error }`
// will fail and fall through to a regular Close — the right
// behaviour given yamux's semantics.
