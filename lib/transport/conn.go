// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package transport abstracts the QUIC carrier under ORP. ORP frames are
// carried on QUIC streams; control on stream 0, data on streams N>0.
// Callers (Agent and Relay) consume Conn/Stream and never see quic-go
// directly — this isolates transport choice from protocol semantics.
package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

// tlsConnectionState is an alias so the Conn interface doesn't have
// to import crypto/tls in callers that don't need it. Same as
// crypto/tls.ConnectionState.
type tlsConnectionState = tls.ConnectionState

// ALPN is the application-layer protocol identifier negotiated at the
// TLS handshake. Both sides advertise it so a misconfigured peer
// (e.g., an HTTP/3 client hitting a relay port) is rejected during the
// handshake instead of leaking into ORP framing.
const ALPN = "orp/1"

// Conn is a multiplexed bidirectional carrier between two peers. The
// underlying transport is QUIC today; the interface stays narrow so the
// implementation can be swapped (e.g., for in-process tests).
type Conn interface {
	// OpenStream creates a new stream initiated by this peer.
	OpenStream(ctx context.Context) (Stream, error)

	// AcceptStream blocks until the remote peer initiates a stream.
	AcceptStream(ctx context.Context) (Stream, error)

	LocalAddr() net.Addr
	RemoteAddr() net.Addr

	// TLS returns the negotiated TLS state. The relay extracts the
	// peer's URI SAN from PeerCertificates here. Callers that don't
	// need it can ignore.
	TLS() tlsConnectionState

	// Close terminates the connection and all in-flight streams. The
	// remote sees the connection close cleanly.
	Close() error
}

// Stream is a single bidirectional byte stream. ORP layers framing on
// top via lib/orp.
type Stream interface {
	io.ReadWriteCloser

	// StreamID is the QUIC stream id; stable for the lifetime of the
	// stream. ORP reserves id 0 for control.
	StreamID() uint64

	// CancelRead aborts the read side. Any in-flight Read call
	// returns an error and subsequent reads return immediately.
	// The peer is signaled (QUIC STOP_SENDING) so it stops sending
	// further bytes on this stream. Used by P2P migration to
	// unblock a bridge parked on a relay-mediated stream after
	// SwapInner has installed the new direct stream — without
	// this hook, the parked Read keeps the bridge listening on
	// the wrong transport indefinitely. code is a transport-
	// specific error code surfaced to the peer's read error.
	CancelRead(code uint64)
}

// Listener accepts incoming Conns.
type Listener interface {
	Accept(ctx context.Context) (Conn, error)
	Addr() net.Addr
	Close() error
}

// Dialer abstracts "open a Conn to addr" so callers (Session.Dial,
// Session.Reconnect, p2p connectivity check) can swap in a shared
// UDP socket — necessary for EIM hole-punching, where the outbound
// QUIC connection's NAT mapping must be the same external endpoint
// a peer dials in to. The default implementation (DefaultDialer)
// opens a fresh UDP socket per Dial; SharedTransport reuses one
// socket across all Dial / Listen calls.
type Dialer interface {
	Dial(ctx context.Context, addr string, tlsConf *tls.Config, qcfg *quic.Config) (Conn, error)
}
