// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package resume provides the building blocks an agent and a relay
// share so that an in-flight ORP stream survives a relay-side
// disconnect or a P2P promotion: stream id generation, a ring buffer
// for unacknowledged bytes, and the per-stream counters that ride on
// STREAM_CHECKPOINT and STREAM_RESUME frames.
package resume

import (
	"hash/maphash"
	"strings"
	"sync/atomic"
	"time"
)

// StreamID is the 64-bit identifier that follows a stream across a
// relay failover and across a P2P promotion or demotion. The
// generator targets <2^-32 collision probability over a single
// agent's lifetime.
type StreamID uint64

// streamIDSeed makes maphash deterministic-per-process but not across
// processes — fine because stream ids only need to be unique within
// the lifetime of a single agent's view.
var streamIDSeed = maphash.MakeSeed()

// monotonicCounter is the per-process tail bytes that prevent two
// streams between the same agents (and tenant) from colliding.
var monotonicCounter atomic.Uint64

// NewStreamID hashes (tenant, sorted agent pair, monotonic counter,
// nanosecond-resolution clock) into a 64-bit id.
//
// The agent pair is sorted so that A->B and B->A on the same logical
// stream produce the same id. Monotonic + ns clock prevent reuse
// even if the same tenant/agents reconnect rapidly.
func NewStreamID(tenant, agentA, agentB string) StreamID {
	a, b := agentA, agentB
	if a > b {
		a, b = b, a
	}
	var h maphash.Hash
	h.SetSeed(streamIDSeed)
	_, _ = h.WriteString(tenant)
	_, _ = h.WriteString("|")
	_, _ = h.WriteString(a)
	_, _ = h.WriteString("|")
	_, _ = h.WriteString(b)
	// Monotonic counter for intra-process uniqueness.
	var ctr [8]byte
	c := monotonicCounter.Add(1)
	for i := range 8 {
		ctr[i] = byte(c >> (8 * i)) // #nosec G115 -- byte slicing of a uint64 is the intended encoding
	}
	_, _ = h.Write(ctr[:])
	// Nanosecond clock guards against process restarts that reset the
	// counter to zero.
	var t [8]byte
	now := uint64(time.Now().UnixNano())
	for i := range 8 {
		t[i] = byte(now >> (8 * i)) // #nosec G115 -- byte slicing of a uint64 is the intended encoding
	}
	_, _ = h.Write(t[:])
	return StreamID(h.Sum64())
}

// String renders the id as a hex digest.
func (s StreamID) String() string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.Grow(16)
	for i := 60; i >= 0; i -= 4 {
		b.WriteByte(hex[(uint64(s)>>uint(i))&0xf])
	}
	return b.String()
}
