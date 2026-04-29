// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package orp implements the OutRelay Protocol (ORP) wire format and
// stream state machine. ORP rides on top of QUIC: stream 0 carries
// control frames, streams N>0 carry per-application data streams.
//
// Each frame is an 8-byte big-endian fixed header followed by a
// protobuf-encoded payload:
//
//	bytes [0:2]  Version(3) | Reserved(2) | Type(11)
//	bytes [2:4]  Flags
//	bytes [4:8]  Length (payload bytes, max 2^24)
//	bytes [8:..] Payload (protobuf-encoded message for the given Type)
//
// Once the relay has paired the two halves of a data stream and
// entered splice mode, framing stops — the relay just copies raw
// bytes between the two QUIC streams. ORP framing therefore only
// applies to control frames (stream 0) and to the negotiation frames
// at the start of each data stream.
package orp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// HeaderSize is the fixed ORP frame header length in bytes.
const HeaderSize = 8

// MaxPayload is the maximum payload size (16 MiB).
const MaxPayload = 1 << 24

// Version is a 3-bit ORP protocol version field.
const (
	Version1       uint8 = 1
	CurrentVersion       = Version1
)

// FrameType is the 11-bit type code identifying a frame.
type FrameType uint16

// Control frames live on stream 0.
const (
	FrameTypeHello          FrameType = 0x001
	FrameTypeHelloAck       FrameType = 0x002
	FrameTypeRegister       FrameType = 0x003
	FrameTypeRegisterAck    FrameType = 0x004
	FrameTypeResolve        FrameType = 0x005
	FrameTypeResolveResp    FrameType = 0x006
	FrameTypeOpenStream     FrameType = 0x010
	FrameTypeIncomingStream FrameType = 0x011
	FrameTypeStreamAccept   FrameType = 0x012
	FrameTypeStreamReject   FrameType = 0x013
	FrameTypePing           FrameType = 0x020
	FrameTypePong           FrameType = 0x021
	FrameTypePolicyUpdate   FrameType = 0x030
	FrameTypeMetricReport   FrameType = 0x040
	FrameTypeGoaway         FrameType = 0x0FF
)

// Data-stream frames (stream N>0). After splice, the framing is dropped.
const (
	FrameTypeStreamInit  FrameType = 0x100
	FrameTypeStreamData  FrameType = 0x101
	FrameTypeStreamFin   FrameType = 0x102
	FrameTypeStreamReset FrameType = 0x103
)

// Stream-resume frames carry the per-stream byte counters that let
// agents pick up after a relay-side reconnect; see lib/resume.
const (
	FrameTypeStreamCheckpoint FrameType = 0x110
	FrameTypeStreamResume     FrameType = 0x111
)

// P2P-promotion frames negotiate a direct agent-to-agent path so
// in-flight streams can migrate off the relay when both sides can
// reach each other.
const (
	FrameTypeObservedAddrQuery FrameType = 0x120
	FrameTypeObservedAddrResp  FrameType = 0x121
	FrameTypeCandidateOffer    FrameType = 0x122
	FrameTypeCandidateAnswer   FrameType = 0x123
	FrameTypeMigrateToP2P      FrameType = 0x124
	FrameTypeMigrateToRelay    FrameType = 0x125
)

// Inter-relay forwarding frames. When a target service lives on a
// peer relay, the local relay opens a new stream to that peer and
// writes FORWARD_STREAM as the first frame. The peer answers with
// STREAM_ACCEPT or STREAM_REJECT on the same stream; on accept,
// both halves enter splice mode.
const (
	FrameTypeForwardStream FrameType = 0x140
)

// Stream-mode signalling frames. After OPEN_STREAM / STREAM_ACCEPT,
// the relay sends exactly one of the two frames below on each
// agent's stream-0 control channel:
//
//   - StreamReady (relay_mode=splice): bytes flow over the
//     relay-mediated stream as usual.
//
//   - AllocGranted (relay_mode=forward): the relay's mini-TURN UDP
//     forwarder is in front of the data plane. Each agent sends
//     opaque UDP packets to the forwarding endpoint with the peer's
//     allocation id as a 4-byte big-endian prefix; the relay strips
//     the prefix and forwards. The agents establish their own
//     end-to-end QUIC over that path so the relay sees only
//     ciphertext.
const (
	FrameTypeAllocGranted FrameType = 0x151
	FrameTypeStreamReady  FrameType = 0x152
)

// maxType is 2^11 - 1 = 0x7FF.
const maxType FrameType = 0x07FF

// maxVersion is 2^3 - 1 = 7.
const maxVersion uint8 = 0x07

var (
	ErrInvalidVersion  = errors.New("orp: invalid version")
	ErrInvalidType     = errors.New("orp: invalid frame type")
	ErrPayloadTooLarge = errors.New("orp: payload exceeds MaxPayload")
	ErrShortHeader     = errors.New("orp: short header")
)

// Frame is a parsed ORP frame. Payload is a protobuf-encoded message
// whose type is identified by Type; callers Unmarshal the payload using
// the matching message in lib/orp/v1.
type Frame struct {
	Version uint8
	Type    FrameType
	Flags   uint16
	Payload []byte
}

// MarshalBinary serializes the frame into wire bytes.
func (f *Frame) MarshalBinary() ([]byte, error) {
	if f.Version > maxVersion {
		return nil, ErrInvalidVersion
	}
	if f.Type > maxType {
		return nil, ErrInvalidType
	}
	if len(f.Payload) > MaxPayload {
		return nil, ErrPayloadTooLarge
	}

	buf := make([]byte, HeaderSize+len(f.Payload))
	// Word 0: Version(3 high bits) | Reserved(2 bits) | Type(11 low bits)
	word0 := (uint16(f.Version&maxVersion) << 13) | uint16(f.Type&maxType)
	binary.BigEndian.PutUint16(buf[0:2], word0)
	binary.BigEndian.PutUint16(buf[2:4], f.Flags)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(f.Payload))) // #nosec G115 -- bounded by MaxPayload check above
	copy(buf[HeaderSize:], f.Payload)
	return buf, nil
}

// ParseFrame reads exactly one frame from r. It returns io.EOF only if r
// returned io.EOF before any header byte was read; a short header read
// returns io.ErrUnexpectedEOF.
func ParseFrame(r io.Reader) (*Frame, error) {
	var hdr [HeaderSize]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || (n == 0 && errors.Is(err, io.EOF)) {
			return nil, err
		}
		return nil, fmt.Errorf("orp: read header: %w", err)
	}

	word0 := binary.BigEndian.Uint16(hdr[0:2])
	flags := binary.BigEndian.Uint16(hdr[2:4])
	length := binary.BigEndian.Uint32(hdr[4:8])

	version := uint8(word0 >> 13)
	typ := FrameType(word0 & uint16(maxType))

	if length > MaxPayload {
		return nil, ErrPayloadTooLarge
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("orp: read payload: %w", err)
		}
	}

	return &Frame{
		Version: version,
		Type:    typ,
		Flags:   flags,
		Payload: payload,
	}, nil
}
