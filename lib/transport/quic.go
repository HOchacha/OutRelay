// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/quic-go/quic-go"
)

// DefaultMaxIdleTimeout bounds how long a QUIC connection may sit
// without observed traffic before either side tears it down. The
// 10 s value is short enough for an agent to notice a dead relay
// within the relay's resume window, long enough that brief packet
// drops do not tear down a healthy link.
const DefaultMaxIdleTimeout = 10 * time.Second

// DefaultKeepAlivePeriod is the interval at which the dialing side
// sends QUIC PING frames so MaxIdleTimeout doesn't fire on a healthy
// link. ~MaxIdleTimeout/3 leaves room for one missed PING before the
// idle timer fires.
const DefaultKeepAlivePeriod = 3 * time.Second

// withDefaults clones qcfg (or constructs one) and applies the
// MaxIdleTimeout / KeepAlivePeriod defaults when the caller didn't
// set them. Caller-supplied non-zero values are preserved verbatim.
func withDefaults(qcfg *quic.Config) *quic.Config {
	var cfg quic.Config
	if qcfg != nil {
		cfg = *qcfg
	}
	if cfg.MaxIdleTimeout == 0 {
		cfg.MaxIdleTimeout = DefaultMaxIdleTimeout
	}
	if cfg.KeepAlivePeriod == 0 {
		cfg.KeepAlivePeriod = DefaultKeepAlivePeriod
	}
	return &cfg
}

// DialQUIC opens a QUIC connection to addr. The caller-supplied tlsConf
// must contain at least one client certificate when mTLS is required;
// ALPN is set automatically if not present. Idle / keepalive defaults
// are applied per withDefaults when qcfg leaves them zero.
func DialQUIC(ctx context.Context, addr string, tlsConf *tls.Config, qcfg *quic.Config) (Conn, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	tlsConf = ensureALPN(tlsConf)
	qc, err := quic.DialAddr(ctx, addr, tlsConf, withDefaults(qcfg))
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}
	return &quicConn{conn: qc}, nil
}

// ListenQUIC opens a UDP-based QUIC listener at addr. Use ":0" for an
// ephemeral port; recover the actual address via Listener.Addr.
func ListenQUIC(addr string, tlsConf *tls.Config, qcfg *quic.Config) (Listener, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	tlsConf = ensureALPN(tlsConf)
	ln, err := quic.ListenAddr(addr, tlsConf, withDefaults(qcfg))
	if err != nil {
		return nil, fmt.Errorf("transport: listen %s: %w", addr, err)
	}
	return &quicListener{ln: ln}, nil
}

// ensureALPN returns a clone of tlsConf with ALPN appended if missing.
func ensureALPN(tlsConf *tls.Config) *tls.Config {
	if slices.Contains(tlsConf.NextProtos, ALPN) {
		return tlsConf
	}
	cp := tlsConf.Clone()
	cp.NextProtos = append(cp.NextProtos, ALPN)
	return cp
}

type quicListener struct {
	ln *quic.Listener
}

func (l *quicListener) Accept(ctx context.Context) (Conn, error) {
	qc, err := l.ln.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return &quicConn{conn: qc}, nil
}

func (l *quicListener) Addr() net.Addr { return l.ln.Addr() }
func (l *quicListener) Close() error   { return l.ln.Close() }

type quicConn struct {
	conn *quic.Conn
}

func (c *quicConn) OpenStream(ctx context.Context) (Stream, error) {
	s, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &quicStream{s: s}, nil
}

func (c *quicConn) AcceptStream(ctx context.Context) (Stream, error) {
	s, err := c.conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return &quicStream{s: s}, nil
}

func (c *quicConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *quicConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// TLS returns the negotiated TLS state. quic-go's ConnectionState
// embeds the TLS handshake info; we surface only that part.
func (c *quicConn) TLS() tls.ConnectionState {
	return c.conn.ConnectionState().TLS
}

func (c *quicConn) Close() error {
	return c.conn.CloseWithError(0, "")
}

type quicStream struct {
	s *quic.Stream
}

func (s *quicStream) Read(p []byte) (int, error)  { return s.s.Read(p) }
func (s *quicStream) Write(p []byte) (int, error) { return s.s.Write(p) }
func (s *quicStream) Close() error                { return s.s.Close() }
func (s *quicStream) StreamID() uint64            { return uint64(s.s.StreamID()) } // #nosec G115 -- quic.StreamID is non-negative
func (s *quicStream) CancelRead(code uint64) {
	s.s.CancelRead(quic.StreamErrorCode(code))
}

// CloseWrite half-closes the write side. quic-go's Stream.Close() is
// already a write-only close (FIN); this alias lets callers
// (splice.Bidirectional, agent bridges) discover the capability via a
// type assertion against an interface{ CloseWrite() error }.
func (s *quicStream) CloseWrite() error { return s.s.Close() }

// DefaultDialer wraps DialQUIC behind the Dialer interface so
// callers that don't need a shared socket pay no per-Dial setup.
type DefaultDialer struct{}

func (DefaultDialer) Dial(ctx context.Context, addr string, tlsConf *tls.Config, qcfg *quic.Config) (Conn, error) {
	return DialQUIC(ctx, addr, tlsConf, qcfg)
}

// SharedTransport binds a single UDP socket and exposes both
// outbound (Dial) and inbound (Listen) QUIC over it. Use one of
// these per agent when EIM hole-punching is desired: the agent's
// connection to the relay determines the NAT mapping for this
// socket, srflx tells the agent that mapping, and a peer dialing
// the mapped external endpoint lands on the SAME socket — quic-go's
// Listen path then accepts the connection. With per-call sockets
// (DefaultDialer + ListenQUIC) the listener and dialer have
// different ports, so srflx-derived addresses point at the wrong
// socket and the connectivity check never succeeds from behind a
// NAT.
type SharedTransport struct {
	t   *quic.Transport
	udp *net.UDPConn
}

// NewSharedTransport binds a UDP socket at addr (e.g. "0.0.0.0:7445")
// and wraps it for shared client+server use. Close releases the
// socket.
func NewSharedTransport(addr string) (*SharedTransport, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: resolve %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("transport: bind %s: %w", addr, err)
	}
	return &SharedTransport{
		t:   &quic.Transport{Conn: conn},
		udp: conn,
	}, nil
}

// Dial opens a QUIC connection to raddr from the shared socket.
// The peer sees this connection as originating from the shared
// socket's local address (post-NAT, the NAT-mapped endpoint).
func (s *SharedTransport) Dial(ctx context.Context, raddr string, tlsConf *tls.Config, qcfg *quic.Config) (Conn, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, fmt.Errorf("transport: resolve %s: %w", raddr, err)
	}
	tlsConf = ensureALPN(tlsConf)
	qc, err := s.t.Dial(ctx, udpAddr, tlsConf, withDefaults(qcfg))
	if err != nil {
		return nil, fmt.Errorf("transport: shared dial %s: %w", raddr, err)
	}
	return &quicConn{conn: qc}, nil
}

// Listen starts accepting inbound QUIC connections on the shared
// socket. Concurrent with Dial; quic-go demultiplexes by
// connection id.
func (s *SharedTransport) Listen(tlsConf *tls.Config, qcfg *quic.Config) (Listener, error) {
	if tlsConf == nil {
		return nil, errors.New("transport: tls.Config required")
	}
	tlsConf = ensureALPN(tlsConf)
	ln, err := s.t.Listen(tlsConf, withDefaults(qcfg))
	if err != nil {
		return nil, fmt.Errorf("transport: shared listen: %w", err)
	}
	return &quicListener{ln: ln}, nil
}

// LocalAddr returns the bound UDP local address. Useful for
// extracting the listener port to pair with a srflx-discovered
// external IP.
func (s *SharedTransport) LocalAddr() net.Addr { return s.udp.LocalAddr() }

// Close releases the underlying UDP socket. Closes both the Dial-
// originated connections and the Listen path implicitly.
func (s *SharedTransport) Close() error {
	return s.t.Close()
}
