// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package resume

import (
	"errors"
	"sync"
)

// DefaultRingCapacity is the 16 MiB default per-stream resume buffer.
// It is used as the fixed cap when NewRingBuffer is given a
// non-positive capacity, and as the upper bound when
// NewAdaptiveRingBuffer is given a non-positive maximum.
const DefaultRingCapacity = 16 * 1024 * 1024

// DefaultMinRingCapacity is the lower bound the adaptive policy
// shrinks toward when a stream sits idle.
const DefaultMinRingCapacity = 64 * 1024

// The adaptive ring grows when an incoming Write would overflow `cap`
// and there is still headroom up to `maxCap`; it shrinks when ring
// occupancy falls below cap/4. The hysteresis between the two
// thresholds prevents oscillation under bursty traffic.

// ErrBeforeRing is returned by BytesFrom when the requested logical
// position has already been evicted from the ring. The caller
// surfaces this as a buffer-overflow flag in STREAM_CHECKPOINT, and
// the resume attempt eventually fails up to the application.
var ErrBeforeRing = errors.New("resume: position evicted from ring")

// RingBuffer keeps the last cap bytes written. Writers push bytes and
// the head counter advances by len(p); on overflow the oldest bytes
// are evicted. BytesFrom returns the slice of bytes from a logical
// position to head.
//
// All public methods are safe for concurrent use by one writer plus
// many BytesFrom readers; concurrent writers must serialize externally
// (the agent's ResumableStream uses a per-stream mutex).
type RingBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
	// minCap and maxCap bound the cap field for the adaptive policy.
	// For a fixed-size ring built via NewRingBuffer, both equal cap,
	// so cap never moves.
	minCap int
	maxCap int
	// head is the total bytes ever written. tail = head - len(buf).
	head int64
}

// NewRingBuffer creates a fixed-capacity buffer. A non-positive
// capacity is normalised to DefaultRingCapacity. Adaptive grow/shrink
// is disabled; cap stays put for the lifetime of the buffer.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingCapacity
	}
	return &RingBuffer{
		buf:    make([]byte, 0, capacity),
		cap:    capacity,
		minCap: capacity,
		maxCap: capacity,
	}
}

// NewAdaptiveRingBuffer creates a buffer that starts at minCap and
// grows up to maxCap on busy traffic, then shrinks back toward minCap
// when occupancy falls below cap/4. A non-positive minCap is
// normalised to DefaultMinRingCapacity; a non-positive maxCap to
// DefaultRingCapacity. If minCap > maxCap, minCap is clamped to
// maxCap (the ring degenerates to fixed cap).
func NewAdaptiveRingBuffer(minCap, maxCap int) *RingBuffer {
	if minCap <= 0 {
		minCap = DefaultMinRingCapacity
	}
	if maxCap <= 0 {
		maxCap = DefaultRingCapacity
	}
	if minCap > maxCap {
		minCap = maxCap
	}
	return &RingBuffer{
		buf:    make([]byte, 0, minCap),
		cap:    minCap,
		minCap: minCap,
		maxCap: maxCap,
	}
}

// Write appends p to the buffer; on overflow, the oldest bytes are
// dropped. The logical position of byte i in p is (head + i) where
// head is the value before this Write. For an adaptive ring, cap
// grows toward maxCap to avoid eviction whenever headroom is
// available.
func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	r.head += int64(n)

	// Adaptive grow: if we have headroom up to maxCap, expand cap so
	// this Write can be retained without eviction.
	if r.maxCap > r.cap && len(r.buf)+n > r.cap {
		needed := len(r.buf) + n
		newCap := min(max(r.cap*2, needed), r.maxCap)
		if newCap > r.cap {
			r.cap = newCap
		}
	}

	// Trim and append; this is O(n) per write but adequate for a
	// fixed-size buffer with bounded throughput. A circular layout
	// is a future optimisation.
	if n >= r.cap {
		r.buf = append(r.buf[:0], p[n-r.cap:]...)
		return n, nil
	}
	if len(r.buf)+n <= r.cap {
		r.buf = append(r.buf, p...)
		return n, nil
	}
	overflow := len(r.buf) + n - r.cap
	r.buf = append(r.buf[overflow:], p...)
	return n, nil
}

// Head returns the total bytes ever written.
func (r *RingBuffer) Head() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.head
}

// Tail is the smallest position currently in the buffer.
func (r *RingBuffer) Tail() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.head - int64(len(r.buf))
}

// BytesFrom returns a copy of bytes from logical position pos to head.
// Returns ErrBeforeRing if pos is older than the oldest byte still
// resident.
func (r *RingBuffer) BytesFrom(pos int64) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tail := r.head - int64(len(r.buf))
	if pos < tail {
		return nil, ErrBeforeRing
	}
	if pos > r.head {
		// Caller asks for bytes we haven't written yet — return empty.
		return []byte{}, nil
	}
	off := int(pos - tail)
	out := make([]byte, len(r.buf)-off)
	copy(out, r.buf[off:])
	return out, nil
}

// Discard frees bytes whose logical position is < before. The agent
// calls this on every checkpoint to release acked bytes back to the
// pool. For an adaptive ring, cap shrinks toward minCap when
// occupancy drops below cap/4.
func (r *RingBuffer) Discard(before int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tail := r.head - int64(len(r.buf))
	if before <= tail {
		return
	}
	drop := int(before - tail)
	if drop >= len(r.buf) {
		r.buf = r.buf[:0]
	} else {
		r.buf = r.buf[drop:]
	}

	// Adaptive shrink: usage well below cap and we are above minCap.
	if r.cap > r.minCap && len(r.buf)*4 < r.cap {
		// Never shrink below current occupancy or below minCap.
		newCap := max(r.cap/2, r.minCap, len(r.buf))
		if newCap < r.cap {
			nb := make([]byte, len(r.buf), newCap)
			copy(nb, r.buf)
			r.buf = nb
			r.cap = newCap
		}
	}
}

// Len returns the number of bytes currently in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// Cap returns the current target capacity. For a fixed-size ring this
// is the value passed to NewRingBuffer. For an adaptive ring this
// floats between minCap and maxCap as the workload changes.
func (r *RingBuffer) Cap() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cap
}
