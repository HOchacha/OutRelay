// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package orp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"testing/quick"
)

// TestFrameRoundtrip is a property-based test: every well-formed Frame
// survives MarshalBinary -> ParseFrame round-trip unchanged.
func TestFrameRoundtrip(t *testing.T) {
	t.Parallel()
	check := func(version uint8, typ uint16, flags uint16, payload []byte) bool {
		// Constrain to legal field widths; the codec must accept any value
		// within these widths and reject anything outside.
		version &= maxVersion
		typ &= uint16(maxType)
		if len(payload) > MaxPayload {
			payload = payload[:MaxPayload]
		}
		original := &Frame{
			Version: version,
			Type:    FrameType(typ),
			Flags:   flags,
			Payload: payload,
		}
		data, err := original.MarshalBinary()
		if err != nil {
			t.Logf("marshal: %v", err)
			return false
		}
		parsed, err := ParseFrame(bytes.NewReader(data))
		if err != nil {
			t.Logf("parse: %v", err)
			return false
		}
		if parsed.Version != original.Version ||
			parsed.Type != original.Type ||
			parsed.Flags != original.Flags ||
			!bytes.Equal(parsed.Payload, original.Payload) {
			t.Logf("mismatch: orig=%+v parsed=%+v", original, parsed)
			return false
		}
		return true
	}
	if err := quick.Check(check, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

// TestFrameRejectsOutOfRange verifies the codec rejects field values
// that overflow their bit width.
func TestFrameRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		f    Frame
		want error
	}{
		{"version overflow", Frame{Version: 8}, ErrInvalidVersion},
		{"type overflow", Frame{Type: 0x800}, ErrInvalidType},
		{"payload too large", Frame{Payload: make([]byte, MaxPayload+1)}, ErrPayloadTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.f.MarshalBinary()
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestParseFrameShortHeader: header read aborts cleanly on EOF.
func TestParseFrameShortHeader(t *testing.T) {
	t.Parallel()
	_, err := ParseFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want io.EOF", err)
	}
	_, err = ParseFrame(bytes.NewReader([]byte{1, 2, 3}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestParseFrameDeclaredLengthMismatch: declared length larger than
// available payload must error.
func TestParseFrameDeclaredLengthMismatch(t *testing.T) {
	t.Parallel()
	// Header claims 100-byte payload but only 5 bytes follow.
	hdr := []byte{
		0x20, 0x01, // version=1, type=0x001 (HELLO)
		0x00, 0x00, // flags=0
		0x00, 0x00, 0x00, 0x64, // length=100
	}
	buf := append(hdr, 1, 2, 3, 4, 5)
	_, err := ParseFrame(bytes.NewReader(buf))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestKnownLayout: a hand-rolled HELLO frame parses with the expected
// fields. Locks the on-the-wire layout so future refactors don't drift.
func TestKnownLayout(t *testing.T) {
	t.Parallel()
	original := &Frame{
		Version: Version1,
		Type:    FrameTypeHello,
		Flags:   0xABCD,
		Payload: []byte("hi"),
	}
	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x20, 0x01, // version=1 (top 3 bits) | type=0x001
		0xAB, 0xCD, // flags
		0x00, 0x00, 0x00, 0x02, // length=2
		'h', 'i',
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("layout mismatch:\n got  %x\n want %x", data, want)
	}
	parsed, err := ParseFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, original) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", parsed, original)
	}
}
