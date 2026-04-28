// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package resume

import (
	"sync/atomic"
)

// CheckpointPeriodMs is the default checkpoint cadence (100 ms).
// The agent's session goroutine wakes up every period and emits one
// StreamCheckpoint per active resumable stream.
const CheckpointPeriodMs = 100

// Counters is the per-stream byte-position state shared between the
// data-plane goroutines (Write / Read) and the control-plane sender
// (StreamCheckpoint emitter). All fields are atomic so checkpointing
// is lock-free.
type Counters struct {
	// SentPos: count of bytes Write has accepted into the stream.
	SentPos atomic.Int64
	// RecvPos: count of bytes Read has delivered to the application.
	RecvPos atomic.Int64
	// PeerAckPos: peer's most recent reported RecvPos (i.e. the byte
	// position up to which the peer has consumed our output). The
	// agent calls Discard(PeerAckPos) on its ring after each update.
	PeerAckPos atomic.Int64
}

// State bundles the ring buffer + counters per stream. The agent's
// session keeps one State per active resumable stream.
type State struct {
	ID       StreamID
	Ring     *RingBuffer
	Counters *Counters
}

// NewState constructs a fresh per-stream state with a ring of the
// given capacity (DefaultRingCapacity if <= 0).
func NewState(id StreamID, ringCap int) *State {
	return &State{
		ID:       id,
		Ring:     NewRingBuffer(ringCap),
		Counters: &Counters{},
	}
}

// Sent returns the snapshot byte counter for write-side accounting.
func (s *State) Sent() int64 { return s.Counters.SentPos.Load() }

// Received returns the snapshot byte counter for read-side accounting.
func (s *State) Received() int64 { return s.Counters.RecvPos.Load() }

// PeerAck returns the snapshot of how far the peer has acknowledged
// receipt of our output bytes.
func (s *State) PeerAck() int64 { return s.Counters.PeerAckPos.Load() }

// OnWrite records that n bytes were just written into the stream and
// pushes them through the ring.
func (s *State) OnWrite(p []byte) {
	if len(p) == 0 {
		return
	}
	_, _ = s.Ring.Write(p)
	s.Counters.SentPos.Add(int64(len(p)))
}

// OnRead records that n bytes were just delivered to the application.
func (s *State) OnRead(n int) {
	if n <= 0 {
		return
	}
	s.Counters.RecvPos.Add(int64(n))
}

// OnCheckpointFromPeer is called when an inbound StreamCheckpoint
// arrives. peerAck is the peer's claimed PeerAckPos (i.e. how many of
// our bytes the peer has consumed). We bump our local PeerAckPos and
// free that range from the ring.
func (s *State) OnCheckpointFromPeer(peerAck int64) {
	prev := s.Counters.PeerAckPos.Load()
	if peerAck > prev {
		s.Counters.PeerAckPos.Store(peerAck)
		s.Ring.Discard(peerAck)
	}
}

// ResumePayload computes the (my_position, peer_ack_position) pair
// the agent emits in STREAM_RESUME after reconnecting.
func (s *State) ResumePayload() (myPos, peerAck int64) {
	return s.Sent(), s.Received()
}

// RetransmitFrom returns the bytes the agent must resend after a
// successful resume — peer.peer_ack_position is the start of the gap.
// On overflow (ring evicted those bytes) returns ErrBeforeRing; the
// caller marks the stream non-resumable and surfaces an error to the
// application.
func (s *State) RetransmitFrom(peerPos int64) ([]byte, error) {
	return s.Ring.BytesFrom(peerPos)
}
