// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package orp

import "fmt"

// StreamState is the per-stream lifecycle state. The transition
// table is documented on `transition` below.
type StreamState int

const (
	StateInit StreamState = iota
	StateOpening
	StateAccepted
	StateSpliced
	StateClosing
	StateClosed
)

// String returns a human-readable state name.
func (s StreamState) String() string {
	switch s {
	case StateInit:
		return "Init"
	case StateOpening:
		return "Opening"
	case StateAccepted:
		return "Accepted"
	case StateSpliced:
		return "Spliced"
	case StateClosing:
		return "Closing"
	case StateClosed:
		return "Closed"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// StreamEvent triggers transitions in the stream FSM.
type StreamEvent int

const (
	// EventOpen — initiator sent OPEN_STREAM, or responder received INCOMING_STREAM.
	EventOpen StreamEvent = iota
	// EventAccept — STREAM_ACCEPT observed (provider acknowledged).
	EventAccept
	// EventSplice — Relay completed pairing and entered splice mode.
	EventSplice
	// EventFin — graceful half-close (FIN observed).
	EventFin
	// EventReset — abort (STREAM_REJECT or STREAM_RESET); always terminates.
	EventReset
)

// String returns a human-readable event name.
func (e StreamEvent) String() string {
	switch e {
	case EventOpen:
		return "Open"
	case EventAccept:
		return "Accept"
	case EventSplice:
		return "Splice"
	case EventFin:
		return "Fin"
	case EventReset:
		return "Reset"
	default:
		return fmt.Sprintf("Unknown(%d)", int(e))
	}
}

// StreamFSM is a small in-memory state machine for a single stream's
// lifecycle. It is not goroutine-safe; callers serialize Apply.
type StreamFSM struct {
	state StreamState
}

// NewStreamFSM returns an FSM in StateInit.
func NewStreamFSM() *StreamFSM { return &StreamFSM{state: StateInit} }

// State returns the current state.
func (f *StreamFSM) State() StreamState { return f.state }

// Apply transitions the FSM according to e. It returns an error and
// leaves the state unchanged when the transition is not allowed.
func (f *StreamFSM) Apply(e StreamEvent) error {
	next, ok := transition(f.state, e)
	if !ok {
		return fmt.Errorf("orp: invalid transition %s --%s--> ?", f.state, e)
	}
	f.state = next
	return nil
}

// transition encodes the FSM table.
//
//	Init     --Open--> Opening
//	Opening  --Accept--> Accepted     --Fin--> Closing
//	Accepted --Splice--> Spliced      --Fin--> Closing
//	Spliced  --Fin--> Closing
//	Closing  --Fin--> Closed
//	(any)    --Reset--> Closed
func transition(s StreamState, e StreamEvent) (StreamState, bool) {
	if e == EventReset {
		return StateClosed, true
	}
	switch s {
	case StateInit:
		if e == EventOpen {
			return StateOpening, true
		}
	case StateOpening:
		switch e {
		case EventAccept:
			return StateAccepted, true
		case EventFin:
			return StateClosing, true
		}
	case StateAccepted:
		switch e {
		case EventSplice:
			return StateSpliced, true
		case EventFin:
			return StateClosing, true
		}
	case StateSpliced:
		if e == EventFin {
			return StateClosing, true
		}
	case StateClosing:
		if e == EventFin {
			return StateClosed, true
		}
	}
	return s, false
}
