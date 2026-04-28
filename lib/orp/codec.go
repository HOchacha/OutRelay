// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package orp

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// ErrTypeMismatch is returned when a Frame's Type does not match the
// expected payload type.
var ErrTypeMismatch = errors.New("orp: frame type mismatch")

// MarshalProto wraps a protobuf payload in a Frame of the given type.
// Most callers should use this rather than constructing Frame literals.
func MarshalProto(typ FrameType, msg proto.Message) (*Frame, error) {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("orp: marshal %v: %w", typ, err)
	}
	return &Frame{
		Version: CurrentVersion,
		Type:    typ,
		Payload: payload,
	}, nil
}

// UnmarshalProto decodes f.Payload into dst. Returns ErrTypeMismatch
// if expectedType is non-zero and f.Type differs.
func UnmarshalProto(f *Frame, expectedType FrameType, dst proto.Message) error {
	if expectedType != 0 && f.Type != expectedType {
		return fmt.Errorf("%w: got %v, want %v", ErrTypeMismatch, f.Type, expectedType)
	}
	if err := proto.Unmarshal(f.Payload, dst); err != nil {
		return fmt.Errorf("orp: unmarshal %v: %w", f.Type, err)
	}
	return nil
}

// WriteFrame marshals msg, wraps it in a Frame, serializes to bytes,
// and writes it to w. Convenience for "open stream + write one frame"
// call sites.
func WriteFrame(w interface {
	Write([]byte) (int, error)
}, typ FrameType, msg proto.Message) error {
	f, err := MarshalProto(typ, msg)
	if err != nil {
		return err
	}
	data, err := f.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
